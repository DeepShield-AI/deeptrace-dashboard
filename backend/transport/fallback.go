package transport

import (
	"io"
	"log"
	"net/http"
)

// RegisterFallback adds the catch-all /api/ handler (for unregistered API paths).
func RegisterFallback(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/", handleAPIFallback(deps))
}

func handleAPIFallback(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var bodyStr string
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			body, _ := io.ReadAll(r.Body)
			bodyStr = string(body)
		}

		// Cache first.
		if deps.Cache != nil {
			if cached := deps.Cache.FindWithBody(r.Method, r.URL.RequestURI(), bodyStr); cached != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Write(cached)
				return
			}
		}

		log.Printf("❓ UNHANDLED %s %s", r.Method, r.URL.Path)
		writeSuccess(w, []interface{}{})
	}
}
