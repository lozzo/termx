package apihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
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
	request := httptest.NewRequest(http.MethodPost, "/api/operator/accounts", nil)
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

func TestLoginResponseAndLogContractRedactsCredentialAndStoreDetails(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("known-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	activeProfile := &cloudv1.AccountProfile{AccountId: "account-known", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE}
	storeFailure := errors.New("postgres lookup sentinel: no rows for outage@example.com; connection refused")
	tests := []struct {
		name        string
		login       string
		store       *loginContractStore
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{name: "unknown account", login: " Unknown@Example.com ", store: &loginContractStore{lookupErr: account.ErrAccountNotFound}, wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated", wantMessage: "账号或凭据无效。"},
		{name: "known wrong password", login: "Known@Example.com", store: &loginContractStore{record: account.Record{Profile: activeProfile, PasswordHash: hash}}, wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated", wantMessage: "账号或凭据无效。"},
		{name: "store lookup error", login: "Outage@Example.com", store: &loginContractStore{lookupErr: storeFailure}, wantStatus: http.StatusServiceUnavailable, wantCode: "service_unavailable", wantMessage: "服务暂时不可用。"},
		{name: "malformed password hash", login: "Malformed@Example.com", store: &loginContractStore{record: account.Record{Profile: activeProfile, PasswordHash: []byte("malformed raw bcrypt hash")}}, wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated", wantMessage: "账号或凭据无效。"},
	}
	var authenticationLog map[string]any
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			accounts, err := account.New(account.Config{Store: test.store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost})
			if err != nil {
				t.Fatal(err)
			}
			limiter := testLoginLimiter(t, loginLimiterConfig{
				globalLimit: 10, clientLimit: 10, accountLimit: 10,
				window: time.Minute, bucketTTL: 5 * time.Minute,
				maxClientBuckets: 10, maxAccountBuckets: 10,
			})
			var logs bytes.Buffer
			handler := &handler{config: Config{Accounts: accounts, PublicOrigin: "https://cloud.example"}, loginLimiter: limiter, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			payload, err := protojson.Marshal(&cloudv1.LoginAccountRequest{Login: test.login, Password: "wrong-password"})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "https://cloud.example/api/account/login", bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "https://cloud.example")
			request.RemoteAddr = "192.0.2.10:443"
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.wantStatus || body["code"] != test.wantCode || body["message"] != test.wantMessage || body["request_id"] == "" || recorder.Header().Get("X-Request-ID") != body["request_id"] {
				t.Fatalf("status=%d header=%q body=%v", recorder.Code, recorder.Header().Get("X-Request-ID"), body)
			}

			var event map[string]any
			if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
				t.Fatalf("decode log %q: %v", logs.String(), err)
			}
			if event["msg"] != "Cloud login request failed" || event["code"] != test.wantCode || event["request_id"] != body["request_id"] || event["path"] != "/api/account/login" || event["status"] != float64(test.wantStatus) {
				t.Fatalf("unexpected login log event: %v", event)
			}
			allowedKeys := map[string]bool{"time": true, "level": true, "msg": true, "request_id": true, "method": true, "path": true, "status": true, "code": true}
			for key := range event {
				if !allowedKeys[key] {
					t.Fatalf("login log contains non-aggregate field %q: %v", key, event)
				}
			}
			combined := recorder.Body.String() + logs.String()
			for _, forbidden := range []string{account.NormalizeLogin(test.login), storeFailure.Error(), account.ErrAccountNotFound.Error(), account.ErrUnauthenticated.Error(), account.ErrLoginUnavailable.Error(), "no rows", "connection refused", "bcrypt"} {
				if strings.Contains(combined, forbidden) {
					t.Fatalf("response or log contains %q: %s", forbidden, combined)
				}
			}
			if test.wantStatus == http.StatusUnauthorized {
				comparable := cloneLogEvent(event)
				if authenticationLog == nil {
					authenticationLog = comparable
				} else if !reflect.DeepEqual(comparable, authenticationLog) {
					t.Fatalf("authentication log differs by credential outcome: got %v want %v", comparable, authenticationLog)
				}
			}
		})
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

func TestAccountErrorsMapToReviewedHTTPAndGRPCStatuses(t *testing.T) {
	tests := []struct {
		err      error
		httpCode int
		grpcCode codes.Code
	}{
		{account.ErrUnauthenticated, http.StatusUnauthorized, codes.Unauthenticated},
		{account.ErrForbidden, http.StatusForbidden, codes.PermissionDenied},
		{account.ErrRecentAuthenticationRequired, http.StatusForbidden, codes.PermissionDenied},
		{account.ErrAccountConflict, http.StatusConflict, codes.Aborted},
		{account.ErrInvalidArgument, http.StatusBadRequest, codes.InvalidArgument},
	}
	for _, test := range tests {
		if got := serviceHTTPStatus(test.err); got != test.httpCode {
			t.Fatalf("serviceHTTPStatus(%v) = %d, want %d", test.err, got, test.httpCode)
		}
		grpcErr := grpcServiceError(context.Background(), test.err)
		if status.Code(grpcErr) != test.grpcCode || !strings.Contains(grpcErr.Error(), "correlation_id=") || strings.Contains(grpcErr.Error(), test.err.Error()) {
			t.Fatalf("gRPC error = %v", grpcErr)
		}
	}
}

func TestSetupErrorsAreRedactedWithCorrelationID(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeError(recorder, http.StatusConflict, account.ErrSetupCredentialInvalid)
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusBadRequest || body["code"] != "setup_invalid" || body["message"] != "一次性凭据无效或已过期。" || body["request_id"] == "" || strings.Contains(recorder.Body.String(), account.ErrSetupCredentialInvalid.Error()) {
		t.Fatalf("status=%d body=%v", recorder.Code, body)
	}
}

type loginContractStore struct {
	account.Store
	record    account.Record
	lookupErr error
}

func (store *loginContractStore) AccountByLogin(context.Context, string) (account.Record, error) {
	return store.record, store.lookupErr
}

func (store *loginContractStore) CreateSession(_ context.Context, record account.Record, _ account.Session, _ time.Time) (account.Record, error) {
	return record, nil
}

func cloneLogEvent(event map[string]any) map[string]any {
	result := make(map[string]any, len(event)-2)
	for key, value := range event {
		if key != "time" && key != "request_id" {
			result[key] = value
		}
	}
	return result
}

var _ account.Store = (*loginContractStore)(nil)
