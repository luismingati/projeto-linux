package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func newRouter(a *api) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /accounts/{id}/balance", instrument("get_balance", a.handleGetBalance))
	mux.HandleFunc("POST /accounts/{id}/transactions", instrument("post_transaction", a.handlePostTransaction))

	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", a.handleHealth)

	return mux
}
