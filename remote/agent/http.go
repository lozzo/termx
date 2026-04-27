package agent

import (
	"context"
	"encoding/json"
	"net/http"
)

type LocalOfferService interface {
	HandleLocalOffer(context.Context, LocalOfferRequest) (*LocalOfferResponse, error)
}

func NewLocalOfferHandler(handler LocalOfferService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req LocalOfferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.SDP == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp required"})
			return
		}

		resp, err := handler.HandleLocalOffer(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
