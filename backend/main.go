package main

import (
	"log"
	"net/http"

	"deeptrace-backend/cache"
	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/config"
	"deeptrace-backend/enum"
	"deeptrace-backend/query"
	"deeptrace-backend/source"
	"deeptrace-backend/transport"
)

func main() {
	cfg := config.Load()

	// Initialize optional services.
	var cch *clickhouse.CHService
	if cfg.ClickHouse != nil {
		cch = clickhouse.New(cfg.ClickHouse)
	}

	ztSvc := client.NewZerotrace(cfg.ZerotraceAddr)
	algoSvc := client.NewAlgorithms(cfg.AlgorithmsAddr)

	// Always available: cache + file-based aggregator.
	cchCache := cache.New(cfg.CacheDir)

	// Build the DataSource priority chain.
	chain := query.NewDataSourceChain()
	ztDS := source.NewZerotraceDataSource(ztSvc)
	chain.AddListSource(ztDS)
	chain.AddTopSource(ztDS)

	chDS := source.NewCHDataSource(cch)
	chain.AddFlowLogSource(chDS)
	chain.AddTraceMapSource(chDS)
	chain.AddTopSource(chDS)
	chain.AddListSource(chDS)

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

	deps := &transport.Dependencies{
		Cache:      cchCache,
		CH:         cch,
		Algorithms: algoSvc,
		Querier:    querierSvc,
		StaticDir:  cfg.StaticDir,
	}

	mux := http.NewServeMux()
	transport.RegisterAll(mux, deps)

	addr := ":" + cfg.Port
	log.Printf("🚀 DeepTrace Backend starting on %s", addr)
	log.Printf("   Static: %s", cfg.StaticDir)
	if deps.Cache != nil {
		log.Printf("   Cache:  %d entries", deps.Cache.Len())
	}
	if deps.CH != nil && deps.CH.Enabled() {
		log.Printf("   ✅ ClickHouse query engine enabled")
	} else {
		log.Printf("   ⚠️  ClickHouse not available (using files)")
	}
	if deps.Querier != nil && deps.Querier.Zerotrace != nil {
		log.Printf("   ✅ Zerotrace-server: %s", cfg.ZerotraceAddr)
	}
	if deps.Algorithms != nil {
		log.Printf("   ✅ Algorithms: %s", cfg.AlgorithmsAddr)
	}
	log.Fatal(http.ListenAndServe(addr, transport.CORS(mux)))
}
