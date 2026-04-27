package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubOfferHandler struct {
	req  LocalOfferRequest
	resp *LocalOfferResponse
	err  error
}

func (s *stubOfferHandler) HandleLocalOffer(_ context.Context, req LocalOfferRequest) (*LocalOfferResponse, error) {
	s.req = req
	return s.resp, s.err
}

func TestLocalOfferHandlerRejectsMissingSDP(t *testing.T) {
	handler := NewLocalOfferHandler(&stubOfferHandler{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rtc/offer", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing sdp, got %d", rec.Code)
	}
}

func TestLocalOfferHandlerReturnsAnswer(t *testing.T) {
	stub := &stubOfferHandler{
		resp: &LocalOfferResponse{SDP: "answer-sdp"},
	}
	handler := NewLocalOfferHandler(stub)

	body, _ := json.Marshal(LocalOfferRequest{SDP: "offer-sdp"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rtc/offer", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if stub.req.SDP != "offer-sdp" {
		t.Fatalf("unexpected forwarded request: %#v", stub.req)
	}

	var resp LocalOfferResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SDP != "answer-sdp" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}
