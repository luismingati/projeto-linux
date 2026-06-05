package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

const requestTimeout = 5 * time.Second

type api struct {
	store Store
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseAccountID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (a *api) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	id, ok := parseAccountID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	resp, err := a.store.GetBalance(ctx, id)
	switch {
	case errors.Is(err, ErrAccountNotFound):
		writeError(w, http.StatusNotFound, "account not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusOK, resp)
	}
}

func (a *api) handlePostTransaction(w http.ResponseWriter, r *http.Request) {
	id, ok := parseAccountID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()

	var req TxRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req = req.Normalize()
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	newBalance, err := a.store.ApplyTransaction(ctx, id, req)
	switch {
	case errors.Is(err, ErrInsufficientFunds):
		transactionsTotal.WithLabelValues(string(req.Type), "rejected").Inc()
		writeError(w, http.StatusUnprocessableEntity, "insufficient funds")
	case err != nil:
		transactionsTotal.WithLabelValues(string(req.Type), "error").Inc()
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		transactionsTotal.WithLabelValues(string(req.Type), "applied").Inc()
		writeJSON(w, http.StatusCreated, map[string]any{
			"account_id":    id,
			"balance_cents": newBalance,
			"type":          req.Type,
			"amount_cents":  req.AmountCents,
		})
	}
}

func (a *api) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
