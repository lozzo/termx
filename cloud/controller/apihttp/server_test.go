package apihttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestReadProtoLimitDetectsLimitPlusOne(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/account/login", strings.NewReader(`{"login":"a"}`))
	if err := readProtoLimit(request, &cloudv1.LoginAccountRequest{}, int64(len(`{"login":"a"}`)-1)); !errors.Is(err, errRequestBodyTooLarge) {
		t.Fatalf("oversize error = %v", err)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/account/login", strings.NewReader(`{"login":"a"}`))
	input := &cloudv1.LoginAccountRequest{}
	if err := readProtoLimit(request, input, int64(len(`{"login":"a"}`))); err != nil || input.GetLogin() != "a" {
		t.Fatalf("exact-limit request login=%q err=%v", input.GetLogin(), err)
	}
}

func TestWriteErrorRedactsDetailsAndReturnsRequestID(t *testing.T) {
	var logs bytes.Buffer
	request := httptest.NewRequest(http.MethodPost, "/api/account/login", nil)
	recorder := httptest.NewRecorder()
	writer := &apiResponseWriter{ResponseWriter: recorder, request: request, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	writer.Header().Set("X-Request-ID", "request-fixed")
	writeError(writer, http.StatusInternalServerError, errors.New("postgres password verifier detail"))

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusInternalServerError || body["code"] != "internal_error" || body["request_id"] != "request-fixed" || recorder.Header().Get("X-Request-ID") != "request-fixed" {
		t.Fatalf("status=%d header=%q body=%v", recorder.Code, recorder.Header().Get("X-Request-ID"), body)
	}
	if strings.Contains(recorder.Body.String(), "postgres") || !strings.Contains(logs.String(), "postgres password verifier detail") || !strings.Contains(logs.String(), "request-fixed") {
		t.Fatalf("response=%s logs=%s", recorder.Body.String(), logs.String())
	}
}

func TestWriteErrorMapsOversizeTo413(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeError(recorder, http.StatusBadRequest, errRequestBodyTooLarge)
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusRequestEntityTooLarge || body["code"] != "payload_too_large" || body["request_id"] == "" {
		t.Fatalf("status=%d body=%v", recorder.Code, body)
	}
}
