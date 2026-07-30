package apihttp

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
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

func TestRecentAuthenticationUsesStableProtocolCodeOnlyForThatError(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      error
		wantCode string
	}{
		{name: "ordinary forbidden", err: account.ErrForbidden, wantCode: "forbidden"},
		{name: "recent authentication", err: account.ErrRecentAuthenticationRequired, wantCode: "recent_auth_required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeError(recorder, http.StatusForbidden, test.err)
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusForbidden || body["code"] != test.wantCode {
				t.Fatalf("status=%d body=%v", recorder.Code, body)
			}

			stream := &testServerTransportStream{}
			ctx := grpc.NewContextWithServerTransportStream(context.Background(), stream)
			_ = grpcServiceError(ctx, test.err)
			codes := stream.trailer.Get("x-error-code")
			if test.wantCode == "recent_auth_required" && (len(codes) != 1 || codes[0] != test.wantCode) {
				t.Fatalf("gRPC trailer=%v", stream.trailer)
			}
			if test.wantCode != "recent_auth_required" && len(codes) != 0 {
				t.Fatalf("ordinary forbidden received recent-auth trailer: %v", stream.trailer)
			}
		})
	}
}

func TestRedeemSetupSetsCookiesAndRedactsSessionSecrets(t *testing.T) {
	store := &setupContractStore{record: account.Record{
		Profile: &cloudv1.AccountProfile{AccountId: "account-setup", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 2},
		Roles:   []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER},
	}}
	accounts, err := account.New(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost})
	if err != nil {
		t.Fatal(err)
	}
	handler := &handler{
		config:       Config{Accounts: accounts, PublicOrigin: "https://cloud.example"},
		setupLimiter: testLoginLimiter(t, loginLimiterConfig{globalLimit: 10, clientLimit: 10, accountLimit: 10, window: time.Minute, bucketTTL: 5 * time.Minute, maxClientBuckets: 10, maxAccountBuckets: 10}),
	}
	payload, err := protojson.Marshal(&cloudv1.RedeemAccountSetupRequest{SetupCredential: strings.Repeat("A", 43), NewPassword: "replacement-password"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://cloud.example/api/account/setup/redeem", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.50:443"
	recorder := httptest.NewRecorder()
	handler.accountPublic(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 3 {
		t.Fatalf("cookies=%v", cookies)
	}
	names := map[string]bool{}
	for _, cookie := range cookies {
		names[cookie.Name] = true
		if !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Value == "" {
			t.Fatalf("invalid setup cookie: %+v", cookie)
		}
	}
	if !names[accessCookieName] || !names[refreshCookieName] || !names[csrfCookieName] {
		t.Fatalf("cookie names=%v", names)
	}
	response := &cloudv1.RedeemAccountSetupResponse{}
	if err := protojson.Unmarshal(recorder.Body.Bytes(), response); err != nil {
		t.Fatal(err)
	}
	if response.GetAccount().GetAccountId() != "account-setup" || len(response.GetRoles()) != 1 || response.GetSession().GetSessionId() == "" || len(response.GetSession().GetAccessToken()) != 0 || len(response.GetSession().GetRefreshToken()) != 0 || len(response.GetSession().GetCsrfToken()) != 0 {
		t.Fatalf("response leaks or omits setup session contract: %v", response)
	}
	if store.session.ID == "" || store.session.AccessDigest == ([sha256.Size]byte{}) || store.session.RefreshDigest == ([sha256.Size]byte{}) {
		t.Fatalf("store did not receive session: %+v", store.session)
	}
}

func TestChangePasswordValidationHTTPAndGRPCContract(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("current-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	profile := &cloudv1.AccountProfile{AccountId: "account-change", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1}
	store := &changePasswordContractStore{record: account.Record{Profile: profile, PasswordHash: hash, CredentialRevision: 1}}
	accounts, err := account.New(account.Config{Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost})
	if err != nil {
		t.Fatal(err)
	}
	identity := account.Identity{Account: profile, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}, SessionID: "session-change"}
	handler := &handler{config: Config{Accounts: accounts}}

	for _, test := range []struct {
		name            string
		currentPassword string
		newPassword     string
		wantStatus      int
		wantCode        string
	}{
		{name: "invalid new password", currentPassword: "current-password", newPassword: strings.Repeat("a", 73), wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "wrong current password takes precedence", currentPassword: "wrong-password", newPassword: strings.Repeat("a", 73), wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload, err := protojson.Marshal(&cloudv1.ChangeAccountPasswordRequest{CurrentPassword: test.currentPassword, NewPassword: test.newPassword})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/account/password", bytes.NewReader(payload))
			request = request.WithContext(account.ContextWithIdentity(request.Context(), identity))
			recorder := httptest.NewRecorder()
			handler.accountPrivate(recorder, request)
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.wantStatus || body["code"] != test.wantCode || body["request_id"] == "" {
				t.Fatalf("status=%d body=%v", recorder.Code, body)
			}
		})
	}
	if store.updated {
		t.Fatal("invalid new password reached UpdatePassword")
	}
	_, serviceErr := accounts.ChangePassword(account.ContextWithIdentity(context.Background(), identity), &cloudv1.ChangeAccountPasswordRequest{CurrentPassword: "current-password", NewPassword: strings.Repeat("a", 73)})
	if !errors.Is(serviceErr, account.ErrInvalidArgument) || status.Code(grpcServiceError(context.Background(), serviceErr)) != codes.InvalidArgument {
		t.Fatalf("service error=%v gRPC=%v", serviceErr, grpcServiceError(context.Background(), serviceErr))
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

type setupContractStore struct {
	account.Store
	record  account.Record
	session account.Session
}

type changePasswordContractStore struct {
	account.Store
	record  account.Record
	updated bool
}

func (store *changePasswordContractStore) AccountByID(context.Context, string) (account.Record, error) {
	return store.record, nil
}

func (store *changePasswordContractStore) UpdatePassword(_ context.Context, _ string, _ account.Record, _ []byte, _ time.Time) (*cloudv1.AccountProfile, error) {
	store.updated = true
	return store.record.Profile, nil
}

func (store *setupContractStore) RedeemAccountSetup(_ context.Context, _ [sha256.Size]byte, _ []byte, session account.Session, _ time.Time) (account.Record, error) {
	store.session = session
	return store.record, nil
}

type testServerTransportStream struct {
	trailer metadata.MD
}

func (*testServerTransportStream) Method() string               { return "/test.Service/Method" }
func (*testServerTransportStream) SetHeader(metadata.MD) error  { return nil }
func (*testServerTransportStream) SendHeader(metadata.MD) error { return nil }
func (stream *testServerTransportStream) SetTrailer(value metadata.MD) error {
	stream.trailer = metadata.Join(stream.trailer, value)
	return nil
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
var _ account.Store = (*setupContractStore)(nil)
var _ account.Store = (*changePasswordContractStore)(nil)
