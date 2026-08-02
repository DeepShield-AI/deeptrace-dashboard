package main

import (
	"net/http"

	"deeptrace-backend/cache"
	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/config"
	"deeptrace-backend/enum"
	"deeptrace-backend/logging"
	"deeptrace-backend/query"
	"deeptrace-backend/query/region"
	"deeptrace-backend/source"
	"deeptrace-backend/transport"
)

func main() {
	cfg := config.Load()

	// Leveled logging: colored stdout + logs/serve.log (level from LOG_LEVEL).
	logging.Init(".")

	// Initialize optional services.
	var cch *clickhouse.CHService
	if cfg.ClickHouse != nil {
		cch = clickhouse.New(cfg.ClickHouse)
	}

	ztSvc := client.NewZerotrace(cfg.ZerotraceAddr)
	algoSvc := client.NewAlgorithms(cfg.AlgorithmsAddr)

	// Always available: cache + file-based aggregator.
	cchCache := cache.New(cfg.CacheDir)

	// Build the DataSource priority chain (ZT → ClickHouse → exact cache).
	chain := query.NewDataSourceChain()
	ztDS := source.NewZerotraceDataSource(ztSvc)
	chain.AddListSource(ztDS)
	chain.AddTopSource(ztDS)

	chDS := source.NewCHDataSource(cch)
	chain.AddTraceMapSource(chDS)
	chain.AddTopSource(chDS)
	chain.AddListSource(chDS)

	cacheDS := source.NewCacheDataSource(cchCache)
	chain.AddListSource(cacheDS)
	chain.AddTopSource(cacheDS)
	chain.AddTraceMapSource(cacheDS)

	// Profile (flame graph) chain: CH → cache. ZT is intentionally NOT
	// registered — deepflow-server cannot serve profile queries (sum()
	// aggregation unsupported, zstd columns corrupted over JSON).
	chain.AddProfileSource(chDS)
	chain.AddProfileSource(cacheDS)

	// Create the query service (central business logic entry point).
	enumSvc := enum.NewEnumService(cch)
	enumSvc.Init()
	chDS.SetEnumService(enumSvc)
	querierSvc := &query.QuerierService{
		Chain:     chain,
		CH:        cch,
		Zerotrace: ztSvc,
		Enum:      enumSvc,
	}

	// Region registry: loaded from deepflow-server's region dictionary.
	// On success it also sets clickhouse.QuerierRegion to the real default name.
	regionSvc := region.New(ztSvc)
	regionSvc.Load()

	deps := &transport.Dependencies{
		Cache:      cchCache,
		CH:         cch,
		Algorithms: algoSvc,
		Querier:    querierSvc,
		Region:     regionSvc,
		StaticDir:  cfg.StaticDir,
	}

	// Optional MySQL metadb access: real user/org for auth handlers when
	// MYSQL_HOST is configured; otherwise the hardcoded identities stay.
	transport.SetUserStore(transport.NewUserStore(cfg.MySQL))

	mux := http.NewServeMux()
	transport.RegisterAll(mux, deps)

	addr := ":" + cfg.Port
	logging.Infof("DeepTrace Backend starting on %s", addr)
	logging.Infof("Static: %s", cfg.StaticDir)
	if cfg.VerifySourceControl {
		logging.Warnf("VERIFY_SOURCE_CONTROL enabled — client-selected data sources allowed (local verification only)")
	}
	if deps.Cache != nil {
		logging.Infof("Cache: %d entries", deps.Cache.Len())
	}
	if deps.CH != nil && deps.CH.Enabled() {
		logging.Infof("ClickHouse query engine enabled")
	} else {
		logging.Warnf("ClickHouse not available (using files)")
	}
	if deps.Querier != nil && deps.Querier.Zerotrace != nil {
		logging.Infof("Zerotrace-server: %s", cfg.ZerotraceAddr)
	}
	if deps.Algorithms != nil {
		logging.Infof("Algorithms: %s", cfg.AlgorithmsAddr)
	}
	handler := transport.CORS(transport.SourceControlMiddleware(cfg.VerifySourceControl)(mux))
	logging.Fatalf("http server: %v", http.ListenAndServe(addr, handler))
}
