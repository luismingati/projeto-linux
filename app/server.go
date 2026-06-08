package main

import (
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func newRouter(a *api) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /accounts/{id}/balance", instrument("get_balance", a.handleGetBalance))
	mux.HandleFunc("POST /accounts/{id}/transactions", instrument("post_transaction", a.handlePostTransaction))

	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", a.handleHealth)

	return servedBy(mux)
}

// servedBy stamps every response with the container hostname so load
// balancing across app instances is visible from the client side.
func servedBy(next http.Handler) http.Handler {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Served-By", host)
		next.ServeHTTP(w, r)
	})
}
