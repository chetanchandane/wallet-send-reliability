package wallet

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// Handler serves the single-account local demo API. It has no user authentication.
func Handler(s *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /balance", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.Balance()
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, http.StatusOK, b)
	})
	mux.HandleFunc("POST /payments", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var req Request
		if err := decoder.Decode(&req); err != nil {
			respondError(w, ErrInvalid)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			respondError(w, ErrInvalid)
			return
		}
		p, err := s.Send(r.Context(), req)
		paymentResponse(w, p, err)
	})
	mux.HandleFunc("GET /payments/{key}", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Get(r.PathValue("key"))
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, http.StatusOK, p)
	})
	mux.HandleFunc("POST /payments/{key}/reconcile", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Reconcile(r.Context(), r.PathValue("key"))
		paymentResponse(w, p, err)
	})
	return mux
}
func paymentResponse(w http.ResponseWriter, p Payment, err error) {
	if err != nil {
		respondError(w, err)
		return
	}
	status := http.StatusAccepted
	if p.State == Confirmed {
		status = http.StatusOK
	}
	if p.State == Failed {
		status = http.StatusUnprocessableEntity
	}
	respond(w, status, p)
}
func respondError(w http.ResponseWriter, err error) {
	status, reason := http.StatusServiceUnavailable, "storage_unavailable"
	switch {
	case errors.Is(err, ErrInvalid):
		status, reason = http.StatusBadRequest, err.Error()
	case errors.Is(err, ErrConflict):
		status, reason = http.StatusConflict, err.Error()
	case errors.Is(err, ErrInsufficient):
		status, reason = http.StatusPaymentRequired, err.Error()
	case errors.Is(err, ErrNotFound):
		status, reason = http.StatusNotFound, err.Error()
	}
	respond(w, status, map[string]string{"error": reason})
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
