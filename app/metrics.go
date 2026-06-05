package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests processed, by route, method and status code.",
	}, []string{"route", "method", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, by route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route"})

	transactionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "transactions_total",
		Help: "Total financial transactions, by type and result.",
	}, []string{"type", "result"})
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func instrument(route string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(rec, r)
		httpDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
	}
}
