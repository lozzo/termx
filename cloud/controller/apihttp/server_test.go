package apihttp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
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
			handler := &handler{
				config: Config{Accounts: accounts, PublicOrigin: "https://cloud.example"}, loginLimiter: limiter,
				logger: slog.New(slog.NewJSONHandler(&logs, nil)), setReadDeadlineForTest: successfulReadDeadline,
			}
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

func TestSlowRequestBodyReturnsStable408WithoutCallingService(t *testing.T) {
	store := &loginContractStore{}
	accounts, err := account.New(account.Config{
		Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour,
		RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := &handler{
		config:          Config{Accounts: accounts, PublicOrigin: "https://cloud.example"},
		loginLimiter:    testLoginLimiter(t, loginLimiterConfig{globalLimit: 10, clientLimit: 10, accountLimit: 10, window: time.Minute, bucketTTL: time.Minute, maxClientBuckets: 10, maxAccountBuckets: 10}),
		bodyReadTimeout: 35 * time.Millisecond,
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	statusCode, header, payload, elapsed := slowBodyHTTPResponse(t, server, "/api/account/login", "application/json")
	if elapsed > 750*time.Millisecond {
		t.Fatalf("slow body timeout took %s", elapsed)
	}
	var body map[string]string
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode timeout response %q: %v", payload, err)
	}
	if statusCode != http.StatusRequestTimeout || body["code"] != "request_timeout" || body["message"] != "请求体读取超时。" || body["request_id"] == "" || header.Get("X-Request-ID") != body["request_id"] {
		t.Fatalf("status=%d header=%q body=%v", statusCode, header.Get("X-Request-ID"), body)
	}
	if strings.Contains(string(payload), "sensitive-slow-login") || strings.Contains(string(payload), errRequestBodyTimeout.Error()) {
		t.Fatalf("timeout response leaked request or internal error: %s", payload)
	}
	if got := store.calls.Load(); got != 0 {
		t.Fatalf("account service store calls = %d, want 0", got)
	}
}

func TestEarlyHTTPRejectionsBoundUnreadSlowBodies(t *testing.T) {
	store := &loginContractStore{}
	accounts, err := account.New(account.Config{
		Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour,
		RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := &handler{
		config: Config{Accounts: accounts, PublicOrigin: "https://cloud.example"}, bodyReadTimeout: 35 * time.Millisecond,
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	for _, test := range []struct {
		name        string
		path        string
		contentType string
		wantStatus  int
		wantCode    string
	}{
		{name: "content type", path: "/api/account/login", contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "authentication", path: "/api/account/password", contentType: "application/json", wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			statusCode, header, payload, elapsed := slowBodyHTTPResponse(t, server, test.path, test.contentType)
			if elapsed > 750*time.Millisecond {
				t.Fatalf("early rejection with unread body took %s", elapsed)
			}
			var body map[string]string
			if err := json.Unmarshal(payload, &body); err != nil {
				t.Fatalf("decode early rejection %q: %v", payload, err)
			}
			if statusCode != test.wantStatus || body["code"] != test.wantCode || body["request_id"] == "" || header.Get("X-Request-ID") != body["request_id"] {
				t.Fatalf("status=%d header=%q body=%v", statusCode, header.Get("X-Request-ID"), body)
			}
		})
	}
	if got := store.calls.Load(); got != 0 {
		t.Fatalf("early rejections called account service store %d times", got)
	}
}

func TestBodyEOFDeadlineDoesNotCancelSlowService(t *testing.T) {
	store := &delayedLoginStore{delay: 120 * time.Millisecond}
	accounts, err := account.New(account.Config{
		Store: store, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour,
		RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: bcrypt.MinCost,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := &handler{
		config: Config{Accounts: accounts, PublicOrigin: "https://cloud.example"},
		loginLimiter: testLoginLimiter(t, loginLimiterConfig{
			globalLimit: 10, clientLimit: 10, accountLimit: 10, window: time.Minute, bucketTTL: time.Minute, maxClientBuckets: 10, maxAccountBuckets: 10,
		}),
		bodyReadTimeout: 25 * time.Millisecond,
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/account/login", strings.NewReader(`{"login":"slow@example.com","password":"wrong-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < store.delay {
		t.Fatalf("service returned before configured delay: %s", elapsed)
	}
	if response.StatusCode != http.StatusUnauthorized || !store.called.Load() || store.canceled.Load() {
		t.Fatalf("status=%d called=%v canceled=%v", response.StatusCode, store.called.Load(), store.canceled.Load())
	}
}

func TestOversizedBodyStillReturns413ThroughDeadlineWrapper(t *testing.T) {
	handler := &handler{config: Config{PublicOrigin: "https://cloud.example"}, setReadDeadlineForTest: successfulReadDeadline}
	request := httptest.NewRequest(http.MethodPost, "/api/account/login", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusRequestEntityTooLarge || body["code"] != "payload_too_large" || body["message"] != "请求体超过大小限制。" || body["request_id"] == "" {
		t.Fatalf("status=%d body=%v", recorder.Code, body)
	}
}

func TestRequestBodyDeadlineStartsAtLifecycleAndClearsAtEOF(t *testing.T) {
	for _, test := range []struct {
		name    string
		method  string
		path    string
		timeout time.Duration
	}{
		{name: "ordinary JSON", method: http.MethodPost, path: "/api/account/login", timeout: 2 * time.Second},
		{name: "certificate create", method: http.MethodPost, path: "/api/operator/certificates", timeout: 7 * time.Second},
		{name: "certificate update", method: http.MethodPut, path: "/api/operator/certificates/profile-1", timeout: 7 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			deadlines := make([]time.Time, 0, 2)
			handler := &handler{
				bodyReadTimeout:        2 * time.Second,
				certificateReadTimeout: 7 * time.Second,
				setReadDeadlineForTest: func(deadline time.Time) error {
					deadlines = append(deadlines, deadline)
					return nil
				},
			}
			request := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
			started := time.Now()
			body, err := handler.limitRequestBodyRead(httptest.NewRecorder(), request)
			if err != nil {
				t.Fatal(err)
			}
			if body == nil {
				t.Fatal("request body was not wrapped")
			}
			if len(deadlines) != 1 {
				t.Fatalf("deadline calls at request lifecycle start = %v", deadlines)
			}
			if got := deadlines[0].Sub(started); got < test.timeout-time.Second || got > test.timeout+time.Second {
				t.Fatalf("read deadline offset = %s, want about %s", got, test.timeout)
			}
			if _, err := io.ReadAll(request.Body); err != nil {
				t.Fatal(err)
			}
			if len(deadlines) != 2 || !deadlines[1].IsZero() {
				t.Fatalf("deadline was not cleared at EOF: %v", deadlines)
			}
			if err := body.Close(); err != nil {
				t.Fatal(err)
			}
			if len(deadlines) != 2 {
				t.Fatalf("Close cleared an already-cleared deadline again: %v", deadlines)
			}
		})
	}
}

func TestRequestBodyDeadlineClearsOnCloseWithoutRead(t *testing.T) {
	deadlines := make([]time.Time, 0, 2)
	handler := &handler{setReadDeadlineForTest: func(deadline time.Time) error {
		deadlines = append(deadlines, deadline)
		return nil
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/account/login", strings.NewReader("unread"))
	body, err := handler.limitRequestBodyRead(httptest.NewRecorder(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if len(deadlines) != 2 || deadlines[0].IsZero() || !deadlines[1].IsZero() {
		t.Fatalf("deadline calls = %v, want set then clear", deadlines)
	}
}

func TestProductionRequestBodyDeadlineDurations(t *testing.T) {
	handler := &handler{setReadDeadlineForTest: func(time.Time) error { return nil }}
	for _, test := range []struct {
		method string
		path   string
		want   time.Duration
	}{
		{method: http.MethodPost, path: "/api/account/login", want: defaultBodyReadTimeout},
		{method: http.MethodPost, path: "/api/install/register", want: defaultBodyReadTimeout},
		{method: http.MethodPost, path: "/api/operator/certificates", want: certificateBodyReadTimeout},
		{method: http.MethodPut, path: "/api/operator/certificates/profile-1", want: certificateBodyReadTimeout},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
		body, err := handler.limitRequestBodyRead(httptest.NewRecorder(), request)
		if err != nil {
			t.Fatal(err)
		}
		if body == nil || body.timeout != test.want {
			t.Fatalf("%s %s timeout = %v, want %s", test.method, test.path, body, test.want)
		}
		if err := body.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGRPCAndGetRequestsAreNotDeadlineWrapped(t *testing.T) {
	grpcServer := grpc.NewServer()
	defer grpcServer.Stop()
	var deadlineCalls atomic.Int32
	handler := &handler{grpcServer: grpcServer, setReadDeadlineForTest: func(time.Time) error {
		deadlineCalls.Add(1)
		return nil
	}}

	grpcRequest := httptest.NewRequest(http.MethodPost, "https://cloud.example/test.Service/Method", strings.NewReader(""))
	grpcRequest.ProtoMajor = 2
	grpcRequest.ProtoMinor = 0
	grpcRequest.Header.Set("Content-Type", "application/grpc")
	grpcBody := grpcRequest.Body
	handler.ServeHTTP(httptest.NewRecorder(), grpcRequest)
	if grpcRequest.Body != grpcBody || deadlineCalls.Load() != 0 {
		t.Fatalf("gRPC body was wrapped or deadline set: body_changed=%v calls=%d", grpcRequest.Body != grpcBody, deadlineCalls.Load())
	}

	for _, path := range []string{"/api/operator/events", "/"} {
		request := httptest.NewRequest(http.MethodGet, path, strings.NewReader("ignored GET body"))
		originalBody := request.Body
		handler.ServeHTTP(httptest.NewRecorder(), request)
		if request.Body != originalBody || deadlineCalls.Load() != 0 {
			t.Fatalf("GET %s body was wrapped or deadline set: body_changed=%v calls=%d", path, request.Body != originalBody, deadlineCalls.Load())
		}
	}
}

func TestUnsupportedResponseWriterAbortsBeforeReadingBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/account/login", nil)
	body := &countingReadCloser{reader: strings.NewReader(`{"login":"must-not-be-read"}`)}
	request.Body = body
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	writer := &opaqueResponseWriter{target: recorder}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		(&handler{}).ServeHTTP(writer, request)
	}()
	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered panic = %v, want http.ErrAbortHandler", recovered)
	}
	if !request.Close || body.reads.Load() != 0 || body.closes.Load() != 0 {
		t.Fatalf("failed deadline request close=%v body reads=%d closes=%d", request.Close, body.reads.Load(), body.closes.Load())
	}
}

func TestReadDeadlineClearFailureAbortsBeforeService(t *testing.T) {
	var setCalls atomic.Int32
	var clearCalls atomic.Int32
	handler := &handler{
		config: Config{PublicOrigin: "https://cloud.example"},
		setReadDeadlineForTest: func(deadline time.Time) error {
			if deadline.IsZero() {
				clearCalls.Add(1)
				return errors.New("injected deadline clear failure")
			}
			setCalls.Add(1)
			return nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/account/login", strings.NewReader(`{"login":"must-not-reach-service"}`))
	request.Header.Set("Content-Type", "application/json")

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}()
	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered panic = %v, want http.ErrAbortHandler", recovered)
	}
	if !request.Close || setCalls.Load() != 1 || clearCalls.Load() == 0 {
		t.Fatalf("clear failure request close=%v set calls=%d clear calls=%d", request.Close, setCalls.Load(), clearCalls.Load())
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
	calls     atomic.Int32
}

type delayedLoginStore struct {
	account.Store
	delay    time.Duration
	called   atomic.Bool
	canceled atomic.Bool
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

type opaqueResponseWriter struct {
	target http.ResponseWriter
}

func (writer *opaqueResponseWriter) Header() http.Header {
	return writer.target.Header()
}

func (writer *opaqueResponseWriter) Write(payload []byte) (int, error) {
	return writer.target.Write(payload)
}

func (writer *opaqueResponseWriter) WriteHeader(status int) {
	writer.target.WriteHeader(status)
}

type countingReadCloser struct {
	reader io.Reader
	reads  atomic.Int32
	closes atomic.Int32
}

func (body *countingReadCloser) Read(buffer []byte) (int, error) {
	body.reads.Add(1)
	return body.reader.Read(buffer)
}

func (body *countingReadCloser) Close() error {
	body.closes.Add(1)
	return nil
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
	store.calls.Add(1)
	return store.record, store.lookupErr
}

func (store *delayedLoginStore) AccountByLogin(ctx context.Context, _ string) (account.Record, error) {
	store.called.Store(true)
	timer := time.NewTimer(store.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return account.Record{}, account.ErrAccountNotFound
	case <-ctx.Done():
		store.canceled.Store(true)
		return account.Record{}, ctx.Err()
	}
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

func slowBodyHTTPResponse(t *testing.T, server *httptest.Server, path, contentType string) (int, http.Header, []byte, time.Duration) {
	t.Helper()
	connection, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = io.WriteString(connection, "POST "+path+" HTTP/1.1\r\nHost: "+server.Listener.Addr().String()+"\r\nContent-Type: "+contentType+"\r\nContent-Length: 1024\r\n\r\n{\"login\":\"sensitive-slow-login@example.com\"")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header.Clone(), payload, time.Since(started)
}

func successfulReadDeadline(time.Time) error { return nil }

var _ account.Store = (*loginContractStore)(nil)
var _ account.Store = (*delayedLoginStore)(nil)
var _ account.Store = (*setupContractStore)(nil)
var _ account.Store = (*changePasswordContractStore)(nil)
