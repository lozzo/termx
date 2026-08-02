// Package apihttp 提供 Controller 浏览器 JSON、安装和静态管理页面 adapter。
// 它只组合 edgeconfig 与 Directory 投影，不持有第二份业务状态。
package apihttp

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"os"
	pathpkg "path"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	"github.com/anytty/anytty/cloud/controller/certificate"
	"github.com/anytty/anytty/cloud/controller/commerce"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/directoryapi"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	"github.com/anytty/anytty/cloud/controller/install"
	operatorservice "github.com/anytty/anytty/cloud/controller/operator"
	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:embed web/*
var webFiles embed.FS

const (
	defaultBodyReadTimeout     = 10 * time.Second
	certificateBodyReadTimeout = 30 * time.Second
)

// Config 是 Controller 原生 HTTPS 管理/安装 listener 的装配输入。
type Config struct {
	ListenAddress      string
	TLSCertificateFile string
	TLSPrivateKeyFile  string
	PublicOrigin       string
	Edges              *edgeconfig.Service
	Directory          *directory.Directory
	Install            *install.Service
	Enrollment         *enrollment.Service
	DaemonManagement   *enrollment.ManagementService
	ClientDirectory    *directoryapi.Service
	Accounts           *account.Service
	Commerce           *commerce.Service
	Control            *control.Service
	Operator           *operatorservice.Service
	Certificates       *certificate.Service
	TrustedProxyCIDRs  []netip.Prefix
	Logger             *slog.Logger
}

// Server 拥有原生 HTTPS listener 生命周期，不拥有 Edge 配置或实时目录。
type Server struct {
	listener            net.Listener
	httpServer          *http.Server
	errors              chan error
	eventSourceShutdown chan struct{}
	shutdownMu          sync.Mutex
	shutdownGate        chan struct{}
	shutdownStarted     bool
	shutdownDone        bool
	shutdownErr         error
}

// Start 验证 TLS/认证配置、绑定 listener 并启动 HTTPS。
func Start(config Config) (*Server, error) {
	config.ListenAddress = strings.TrimSpace(config.ListenAddress)
	if config.ListenAddress == "" || config.Edges == nil || config.Directory == nil || config.Install == nil || config.Accounts == nil || config.Commerce == nil || config.Control == nil || config.Operator == nil || config.Certificates == nil {
		return nil, errors.New("HTTP listen and R7 application services are required")
	}
	tlsConfig, err := securetransport.NewServerTLSConfig(securetransport.ServerOptions{CertificateFile: config.TLSCertificateFile, PrivateKeyFile: config.TLSPrivateKeyFile})
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen Controller HTTPS: %w", err)
	}
	eventSourceShutdown := make(chan struct{})
	handler, err := newHandler(config, eventSourceShutdown)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	server := &Server{listener: listener, errors: make(chan error, 1), eventSourceShutdown: eventSourceShutdown}
	server.httpServer = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, TLSConfig: tlsConfig}
	go func() {
		if err := server.httpServer.ServeTLS(listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			server.errors <- err
		}
	}()
	return server, nil
}

// Address 返回实际绑定地址，供部署日志和 integration harness 使用。
func (server *Server) Address() string { return server.listener.Addr().String() }

// Errors 返回 listener 致命错误。
func (server *Server) Errors() <-chan error { return server.errors }

// Shutdown 停止接收新的浏览器和安装请求。
func (server *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("Controller HTTP Server shutdown requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	shutdownGate := server.shutdownGateChannel()
	select {
	case shutdownGate <- struct{}{}:
		defer func() { <-shutdownGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if server.shutdownDone {
		return server.shutdownErr
	}
	if !server.shutdownStarted {
		if server.eventSourceShutdown != nil {
			close(server.eventSourceShutdown)
		}
		server.shutdownStarted = true
	}
	shutdownErr := server.httpServer.Shutdown(ctx)
	if errors.Is(shutdownErr, context.Canceled) || errors.Is(shutdownErr, context.DeadlineExceeded) {
		return shutdownErr
	}
	server.shutdownErr = shutdownErr
	server.shutdownDone = true
	return server.shutdownErr
}

func (server *Server) shutdownGateChannel() chan struct{} {
	server.shutdownMu.Lock()
	defer server.shutdownMu.Unlock()
	if server.shutdownGate == nil {
		server.shutdownGate = make(chan struct{}, 1)
	}
	return server.shutdownGate
}

// NewHandler 构造可测试的 HTTP adapter；调用方仍必须在生产使用 TLS listener。
func NewHandler(config Config) (http.Handler, error) {
	return newHandler(config, nil)
}

func newHandler(config Config, eventSourceShutdown <-chan struct{}) (http.Handler, error) {
	config.PublicOrigin = strings.TrimRight(strings.TrimSpace(config.PublicOrigin), "/")
	if config.Edges == nil || config.Directory == nil || config.Install == nil || config.PublicOrigin == "" || config.Accounts == nil || config.Commerce == nil || config.Control == nil || config.Operator == nil || config.Certificates == nil {
		return nil, errors.New("HTTP handler and R7 application services are required")
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	immutableAssetPaths, err := loadViteAssetPaths(webFiles)
	if err != nil {
		return nil, fmt.Errorf("load embedded Cloud web asset manifest: %w", err)
	}
	var grpcServer *grpc.Server
	if config.Enrollment != nil {
		grpcServer = grpc.NewServer(grpc.UnaryInterceptor(accountUnaryInterceptor(config.Accounts)))
		cloudv1.RegisterEnrollmentServiceServer(grpcServer, config.Enrollment)
		if config.DaemonManagement != nil {
			cloudv1.RegisterDaemonManagementServiceServer(grpcServer, config.DaemonManagement)
		}
		if config.ClientDirectory != nil {
			cloudv1.RegisterDirectoryServiceServer(grpcServer, config.ClientDirectory)
		}
		cloudv1.RegisterCommerceServiceServer(grpcServer, config.Commerce)
		cloudv1.RegisterOperatorServiceServer(grpcServer, config.Operator)
	}
	handler := &handler{config: config, grpcServer: grpcServer, loginLimiter: newDefaultLoginLimiter(), setupLimiter: newDefaultSetupLimiter(), trustedProxyCIDRs: append([]netip.Prefix(nil), config.TrustedProxyCIDRs...), logger: config.Logger, staticFiles: webFiles, immutableAssetPaths: immutableAssetPaths, eventSourceShutdown: eventSourceShutdown}
	return handler, nil
}

type handler struct {
	config              Config
	grpcServer          *grpc.Server
	loginLimiter        *loginLimiter
	setupLimiter        *loginLimiter
	trustedProxyCIDRs   []netip.Prefix
	logger              *slog.Logger
	staticFiles         fs.FS
	immutableAssetPaths map[string]struct{}
	eventSourceShutdown <-chan struct{}

	// Package-private hooks keep production deadlines fixed while allowing fast, deterministic tests.
	bodyReadTimeout        time.Duration
	certificateReadTimeout time.Duration
	setReadDeadlineForTest func(time.Time) error
}

func (handler *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'")
	if handler.grpcServer != nil && request.ProtoMajor == 2 && strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/grpc") {
		handler.grpcServer.ServeHTTP(writer, request)
		return
	}
	lifecycleRequest := request
	body, err := handler.limitRequestBodyRead(writer, lifecycleRequest)
	if errors.Is(err, errRequestBodyConnectionAborted) {
		return
	}
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	if body != nil {
		defer func() {
			if body.deadlineActive() {
				lifecycleRequest.Close = true
			}
			_ = body.Close()
		}()
	}
	requestID := uuid.NewString()
	writer.Header().Set("X-Request-ID", requestID)
	writer = &apiResponseWriter{ResponseWriter: writer, request: request, logger: handler.logger}
	switch {
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/install/edge/"):
		handler.installScript(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/artifacts/anytty-cloud-edge-linux-amd64":
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write(handler.config.Install.Artifact())
	case request.Method == http.MethodPost && request.URL.Path == "/api/install/register":
		handler.register(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/api/commerce/plans":
		// 已发布套餐是公开产品目录；未登录请求不能设置 include_unpublished。
		response, err := handler.config.Commerce.ListPlans(request.Context(), &cloudv1.ListPlansRequest{})
		writeServiceResult(writer, response, err)
	case request.URL.Path == "/api/account/login" || request.URL.Path == "/api/account/refresh" || request.URL.Path == "/api/account/setup/redeem":
		if !handler.allowMutationOrigin(writer, request) {
			return
		}
		handler.accountPublic(writer, request)
	case strings.HasPrefix(request.URL.Path, "/api/"):
		identity, ok := handler.authenticate(writer, request)
		if !ok {
			return
		}
		request = request.WithContext(account.ContextWithIdentity(request.Context(), identity))
		if !handler.allowMutationOrigin(writer, request) {
			return
		}
		switch {
		case strings.HasPrefix(request.URL.Path, "/api/account/"):
			handler.accountPrivate(writer, request)
		case strings.HasPrefix(request.URL.Path, "/api/commerce/"):
			handler.commerce(writer, request)
		case strings.HasPrefix(request.URL.Path, "/api/daemons"):
			handler.daemons(writer, request)
		case strings.HasPrefix(request.URL.Path, "/api/operator/"):
			if !identity.HasRole(cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR) {
				writeError(writer, http.StatusForbidden, errors.New("operator role is required"))
				return
			}
			handler.operator(writer, request)
		default:
			writeError(writer, http.StatusNotFound, errors.New("API endpoint was not found"))
		}
	default:
		// SPA shell 与静态资源不包含业务数据；深链刷新后由 API 自动续期 Access JWT，再由 RBAC 守住数据边界。
		handler.serveStatic(writer, request)
	}
}

func (handler *handler) serveStatic(writer http.ResponseWriter, request *http.Request) {
	if handler.immutableAssetPaths == nil {
		http.Error(writer, "Cloud web asset manifest is unavailable", http.StatusInternalServerError)
		return
	}
	cleanPath := pathpkg.Clean("/" + request.URL.Path)
	if cleanPath == "/"+viteAssetManifestName {
		http.NotFound(writer, request)
		return
	}
	extension := pathpkg.Ext(cleanPath)
	name := "web/index.html"
	contentType := "text/html; charset=utf-8"
	if extension != "" && cleanPath != "/index.html" {
		name = "web/" + strings.TrimPrefix(cleanPath, "/")
		contentType = mime.TypeByExtension(extension)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
	}
	staticFiles := handler.staticFiles
	if staticFiles == nil {
		staticFiles = webFiles
	}
	payload, err := fs.ReadFile(staticFiles, name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	if name == "web/index.html" {
		writer.Header().Set("Cache-Control", "no-cache")
	} else if _, immutable := handler.immutableAssetPaths[cleanPath]; immutable {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		writer.Header().Set("Cache-Control", "public, max-age=300")
	}
	_, _ = writer.Write(payload)
}

func (handler *handler) operator(writer http.ResponseWriter, request *http.Request) {
	if handler.operatorR7(writer, request) {
		return
	}
	if request.URL.Path == "/api/operator/daemons" && handler.config.Enrollment != nil {
		switch request.Method {
		case http.MethodGet:
			response, err := handler.config.Enrollment.ListManagedDaemons(request.Context())
			if err != nil {
				writeError(writer, http.StatusInternalServerError, err)
				return
			}
			writeProto(writer, http.StatusOK, response)
		case http.MethodPost:
			input := &cloudv1.CreateDaemonEnrollmentRequest{}
			if err := readProto(request, input); err != nil {
				writeError(writer, http.StatusBadRequest, err)
				return
			}
			response, err := handler.config.Enrollment.CreateEnrollment(request.Context(), input, "anytty cloud enroll --controller "+handler.config.PublicOrigin)
			if err != nil {
				writeError(writer, http.StatusBadRequest, err)
				return
			}
			writeProto(writer, http.StatusCreated, response)
		default:
			writeError(writer, http.StatusMethodNotAllowed, errors.New("operator daemon endpoint method is not allowed"))
		}
		return
	}
	if request.URL.Path == "/api/operator/edges" {
		switch request.Method {
		case http.MethodGet:
			handler.listEdges(writer, request)
		case http.MethodPost:
			handler.createEdge(writer, request)
		default:
			writeError(writer, http.StatusMethodNotAllowed, errors.New("operator edge endpoint method is not allowed"))
		}
		return
	}
	if request.Method == http.MethodPut && strings.HasPrefix(request.URL.Path, "/api/operator/edges/") {
		handler.updateEdge(writer, request)
		return
	}
	if request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/api/operator/edges/") {
		handler.deleteEdge(writer, request)
		return
	}
	writeError(writer, http.StatusNotFound, errors.New("operator endpoint was not found"))
}

func (handler *handler) listEdges(writer http.ResponseWriter, request *http.Request) {
	edges, err := handler.config.Edges.ListEdges(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	runtimeEdges, err := handler.config.Directory.ListEdges(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	runtimeByID := make(map[string]directory.EdgeProjection, len(runtimeEdges))
	for _, runtimeEdge := range runtimeEdges {
		runtimeByID[runtimeEdge.EdgeID] = runtimeEdge
	}
	response := &cloudv1.ListEdgesResponse{Edges: make([]*cloudv1.ManagedEdge, 0, len(edges))}
	for _, edge := range edges {
		binding, bindErr := handler.config.Certificates.BindingForEdge(request.Context(), edge.ID)
		if bindErr != nil {
			writeError(writer, http.StatusInternalServerError, bindErr)
			return
		}
		response.Edges = append(response.Edges, projectEdge(edge, runtimeByID[edge.ID], binding))
	}
	writeProto(writer, http.StatusOK, response)
}

func (handler *handler) createEdge(writer http.ResponseWriter, request *http.Request) {
	input := &cloudv1.CreateEdgeRequest{}
	if err := readProto(request, input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	edge, claim, expiresAt, err := handler.config.Edges.CreateEdge(request.Context(), edgeconfig.CreateInput{Name: input.GetName(), Region: input.GetRegion(), Capacity: input.GetCapacity(), PublicEndpoint: input.GetPublicEndpoint()})
	if err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	response := &cloudv1.CreateEdgeResponse{Edge: projectEdge(edge, directory.EdgeProjection{}, nil), InstallCommand: "curl -fsSL " + handler.config.PublicOrigin + "/install/edge/" + claim + " | sudo sh", ClaimExpiresAt: timestamppb.New(expiresAt)}
	writeProto(writer, http.StatusCreated, response)
}

func (handler *handler) updateEdge(writer http.ResponseWriter, request *http.Request) {
	input := &cloudv1.UpdateEdgeRequest{}
	if err := readProto(request, input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	pathID := strings.TrimPrefix(request.URL.Path, "/api/operator/edges/")
	if input.GetEdgeId() != pathID {
		writeError(writer, http.StatusBadRequest, errors.New("Edge path and request IDs differ"))
		return
	}
	if err := handler.config.Certificates.ValidateEdgeEndpoint(request.Context(), input.GetEdgeId(), input.GetPublicEndpoint()); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	edge, err := handler.config.Edges.UpdateEdge(request.Context(), edgeconfig.UpdateInput{EdgeID: input.GetEdgeId(), ExpectedRevision: input.GetExpectedRevision(), Name: input.GetName(), Region: input.GetRegion(), Capacity: input.GetCapacity(), PublicEndpoint: input.GetPublicEndpoint(), Enabled: input.GetEnabled()})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, edgeconfig.ErrRevisionConflict) {
			status = http.StatusConflict
		}
		writeError(writer, status, err)
		return
	}
	if !edge.Enabled {
		handler.config.Control.InvalidateEdge(edge.ID)
	}
	runtimeProjection := directory.EdgeProjection{}
	if edge.Enabled {
		runtimeProjection, _, _ = handler.config.Directory.Edge(request.Context(), edge.ID)
	}
	binding, err := handler.config.Certificates.BindingForEdge(request.Context(), edge.ID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeProto(writer, http.StatusOK, &cloudv1.UpdateEdgeResponse{Edge: projectEdge(edge, runtimeProjection, binding)})
}

func (handler *handler) deleteEdge(writer http.ResponseWriter, request *http.Request) {
	input := &cloudv1.DeleteEdgeRequest{}
	if err := readProto(request, input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	pathID := strings.TrimPrefix(request.URL.Path, "/api/operator/edges/")
	if input.GetEdgeId() != pathID || strings.Contains(pathID, "/") {
		writeError(writer, http.StatusBadRequest, errors.New("Edge path and request IDs differ"))
		return
	}
	response, err := handler.config.Operator.DeleteEdge(request.Context(), input)
	writeServiceResult(writer, response, err)
}

func (handler *handler) installScript(writer http.ResponseWriter, request *http.Request) {
	token := strings.TrimPrefix(request.URL.Path, "/install/edge/")
	script, err := handler.config.Install.InstallScript(request.Context(), token)
	if err != nil {
		writeError(writer, http.StatusGone, err)
		return
	}
	writer.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, script)
}

func (handler *handler) register(writer http.ResponseWriter, request *http.Request) {
	input := &cloudv1.RegisterEdgeRequest{}
	if err := readProto(request, input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	response, err := handler.config.Install.Register(request.Context(), input)
	if err != nil {
		writeError(writer, http.StatusForbidden, err)
		return
	}
	writeProto(writer, http.StatusOK, response)
}

func (handler *handler) allowMutationOrigin(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		return true
	}
	if contentType := request.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		writeError(writer, http.StatusUnsupportedMediaType, errors.New("application/json is required"))
		return false
	}
	origin := strings.TrimRight(strings.TrimSpace(request.Header.Get("Origin")), "/")
	if origin != "" && origin != handler.config.PublicOrigin {
		writeError(writer, http.StatusForbidden, errors.New("request origin is not allowed"))
		return false
	}
	if request.URL.Path == "/api/account/refresh" {
		csrfCookie, err := request.Cookie(csrfCookieName)
		if err != nil || csrfCookie.Value == "" || csrfCookie.Value != strings.TrimSpace(request.Header.Get("X-AnyTTY-CSRF")) {
			writeError(writer, http.StatusForbidden, errors.New("CSRF proof is invalid"))
			return false
		}
	} else if strings.HasPrefix(request.URL.Path, "/api/") && request.URL.Path != "/api/account/login" && request.URL.Path != "/api/account/setup/redeem" {
		identity, ok := account.IdentityFromContext(request.Context())
		csrfCookie, err := request.Cookie(csrfCookieName)
		csrfHeader := strings.TrimSpace(request.Header.Get("X-AnyTTY-CSRF"))
		if !ok || err != nil || csrfCookie.Value == "" || csrfHeader == "" || csrfCookie.Value != csrfHeader || !validateEncodedCSRF(identity, csrfHeader) {
			writeError(writer, http.StatusForbidden, errors.New("CSRF proof is invalid"))
			return false
		}
	}
	return true
}

func accountUnaryInterceptor(accounts *account.Service) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if strings.Contains(info.FullMethod, "EnrollmentService") || strings.Contains(info.FullMethod, "DirectoryService") {
			return handler(ctx, request)
		}
		values := metadata.ValueFromIncomingContext(ctx, "authorization")
		if len(values) != 1 {
			return nil, grpcServiceError(ctx, account.ErrUnauthenticated)
		}
		token, err := decodeBearer(values[0])
		if err != nil {
			return nil, grpcServiceError(ctx, account.ErrUnauthenticated)
		}
		identity, err := accounts.AuthenticateAccess(ctx, token)
		if err != nil {
			return nil, grpcServiceError(ctx, err)
		}
		response, err := handler(account.ContextWithIdentity(ctx, identity), request)
		return response, grpcServiceError(ctx, err)
	}
}

func grpcServiceError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	correlationID := uuid.NewString()
	trailer := metadata.Pairs("x-correlation-id", correlationID)
	if errors.Is(err, account.ErrRecentAuthenticationRequired) {
		trailer.Append("x-error-code", "recent_auth_required")
	}
	_ = grpc.SetTrailer(ctx, trailer)
	code := codes.Internal
	switch {
	case errors.Is(err, account.ErrUnauthenticated):
		code = codes.Unauthenticated
	case errors.Is(err, account.ErrForbidden), errors.Is(err, account.ErrRecentAuthenticationRequired), errors.Is(err, account.ErrAccountDisabled):
		code = codes.PermissionDenied
	case errors.Is(err, account.ErrInvalidArgument), errors.Is(err, account.ErrSetupCredentialInvalid):
		code = codes.InvalidArgument
	case errors.Is(err, account.ErrAccountConflict):
		code = codes.Aborted
	}
	return status.Errorf(code, "request failed; correlation_id=%s", correlationID)
}

func projectEdge(edge edgeconfig.Edge, runtime directory.EdgeProjection, certificate *cloudv1.CertificateBinding) *cloudv1.ManagedEdge {
	projection := &cloudv1.ManagedEdge{Config: &cloudv1.EdgeDesiredConfig{EdgeId: edge.ID, Version: edge.ConfigVersion, Name: edge.Name, Region: edge.Region, Capacity: edge.Capacity, PublicEndpoint: edge.PublicEndpoint, Enabled: edge.Enabled}, ConfigRevision: edge.Revision, Runtime: &cloudv1.EdgeRuntimeProjection{}, Certificate: certificate}
	if runtime.EdgeID != "" {
		projection.Runtime = &cloudv1.EdgeRuntimeProjection{Online: true, BootId: runtime.BootID, ConnectionId: runtime.ConnectionID, SoftwareVersion: runtime.SoftwareVersion, RuntimeRevision: runtime.RuntimeRevision, AgentCount: uint64(runtime.AgentCount), SessionCount: uint64(runtime.SessionCount), ConnectedAt: timestamppb.New(runtime.ConnectedAt), LastHeartbeat: timestamppb.New(runtime.LastHeartbeat)}
	}
	return projection
}

func readProto(request *http.Request, message proto.Message) error {
	return readProtoLimit(request, message, 1<<20)
}

func readProtoLimit(request *http.Request, message proto.Message, limit int64) error {
	defer request.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(payload)) > limit {
		return errRequestBodyTooLarge
	}
	return (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, message)
}

func (handler *handler) limitRequestBodyRead(writer http.ResponseWriter, request *http.Request) (*lazyReadDeadlineBody, error) {
	if request == nil || request.Body == nil || request.Body == http.NoBody || !requestBodyMayBeRead(request) {
		return nil, nil
	}
	timeout := handler.bodyReadTimeout
	if timeout <= 0 {
		timeout = defaultBodyReadTimeout
	}
	if certificateUploadRequest(request) {
		timeout = handler.certificateReadTimeout
		if timeout <= 0 {
			timeout = certificateBodyReadTimeout
		}
	}
	setReadDeadline := handler.setReadDeadlineForTest
	if setReadDeadline == nil {
		controller := http.NewResponseController(writer)
		setReadDeadline = controller.SetReadDeadline
		if err := setReadDeadline(time.Now().Add(timeout)); err != nil {
			// net/http drains small unread bodies after either a return or an abort;
			// a closed hijacked connection bypasses both drain paths.
			connection, _, hijackErr := controller.Hijack()
			if hijackErr != nil {
				return nil, errRequestBodyDeadlineUnavailable
			}
			_ = connection.Close()
			return nil, errRequestBodyConnectionAborted
		}
	} else if err := setReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, errRequestBodyDeadlineUnavailable
	}
	body := &lazyReadDeadlineBody{ReadCloser: request.Body, timeout: timeout, setReadDeadline: setReadDeadline, deadlineSet: true}
	request.Body = body
	return body, nil
}

func requestBodyMayBeRead(request *http.Request) bool {
	if !strings.HasPrefix(request.URL.Path, "/api/") && !strings.HasPrefix(request.URL.Path, "/install/") {
		return false
	}
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func certificateUploadRequest(request *http.Request) bool {
	return request.Method == http.MethodPost && request.URL.Path == "/api/operator/certificates" ||
		request.Method == http.MethodPut && strings.HasPrefix(request.URL.Path, "/api/operator/certificates/")
}

type lazyReadDeadlineBody struct {
	io.ReadCloser
	timeout         time.Duration
	setReadDeadline func(time.Time) error
	mu              sync.Mutex
	deadlineSet     bool
	closeOnce       sync.Once
	closeErr        error
}

func (body *lazyReadDeadlineBody) Read(buffer []byte) (int, error) {
	read, err := body.ReadCloser.Read(buffer)
	if bodyReadTimedOut(err) {
		return read, errRequestBodyTimeout
	}
	if errors.Is(err, io.EOF) {
		if clearErr := body.clearDeadline(); clearErr != nil {
			panic(http.ErrAbortHandler)
		}
	}
	return read, err
}

func (body *lazyReadDeadlineBody) Close() error {
	body.closeOnce.Do(func() {
		body.closeErr = body.ReadCloser.Close()
	})
	if err := body.clearDeadline(); err != nil {
		panic(http.ErrAbortHandler)
	}
	if bodyReadTimedOut(body.closeErr) {
		return errRequestBodyTimeout
	}
	return body.closeErr
}

func (body *lazyReadDeadlineBody) deadlineActive() bool {
	body.mu.Lock()
	defer body.mu.Unlock()
	return body.deadlineSet
}

func (body *lazyReadDeadlineBody) clearDeadline() error {
	body.mu.Lock()
	defer body.mu.Unlock()
	if !body.deadlineSet {
		return nil
	}
	if err := body.setReadDeadline(time.Time{}); err != nil {
		return errRequestBodyDeadlineUnavailable
	}
	body.deadlineSet = false
	return nil
}

func bodyReadTimedOut(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var timeoutError net.Error
	return errors.As(err, &timeoutError) && timeoutError.Timeout()
}

func writeProto(writer http.ResponseWriter, status int, message proto.Message) {
	payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(message)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}

func writeError(writer http.ResponseWriter, status int, err error) {
	if errors.Is(err, errRequestBodyTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	if errors.Is(err, errRequestBodyTimeout) {
		status = http.StatusRequestTimeout
	}
	if errors.Is(err, errLoginRateLimited) {
		status = http.StatusTooManyRequests
	}
	code, message := publicHTTPError(status)
	if errors.Is(err, errSetupRateLimited) {
		status, code, message = http.StatusTooManyRequests, "setup_rate_limited", "尝试过于频繁，请稍后重试。"
	}
	if errors.Is(err, account.ErrSetupCredentialInvalid) {
		status, code, message = http.StatusBadRequest, "setup_invalid", "一次性凭据无效或已过期。"
	}
	if errors.Is(err, account.ErrRecentAuthenticationRequired) {
		status, code, message = http.StatusForbidden, "recent_auth_required", "需要重新验证身份。"
	}
	requestID := writer.Header().Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
		writer.Header().Set("X-Request-ID", requestID)
	}
	if wrapped, ok := writer.(*apiResponseWriter); ok && wrapped.logger != nil {
		if wrapped.request.URL.Path == "/api/account/login" {
			wrapped.logger.Warn("Cloud login request failed", "request_id", requestID, "method", wrapped.request.Method, "path", wrapped.request.URL.Path, "status", status, "code", code)
		} else {
			wrapped.logger.Warn("Cloud HTTP request failed", "request_id", requestID, "method", wrapped.request.Method, "path", wrapped.request.URL.Path, "status", status, "error", err)
		}
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"code": code, "message": message, "request_id": requestID})
}

var (
	errRequestBodyTooLarge            = errors.New("request body exceeds limit")
	errRequestBodyTimeout             = errors.New("request body read timed out")
	errRequestBodyDeadlineUnavailable = errors.New("request body read deadline is unavailable")
	errRequestBodyConnectionAborted   = errors.New("request body connection was aborted")
)

type apiResponseWriter struct {
	http.ResponseWriter
	request *http.Request
	logger  *slog.Logger
}

func (writer *apiResponseWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func publicHTTPError(status int) (string, string) {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request", "请求无效。"
	case http.StatusUnauthorized:
		return "unauthenticated", "账号或凭据无效。"
	case http.StatusForbidden:
		return "forbidden", "无权执行此操作。"
	case http.StatusNotFound:
		return "not_found", "请求的资源不存在。"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed", "请求方法不受支持。"
	case http.StatusConflict:
		return "conflict", "请求与当前状态冲突。"
	case http.StatusGone:
		return "gone", "请求的资源已失效。"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large", "请求体超过大小限制。"
	case http.StatusRequestTimeout:
		return "request_timeout", "请求体读取超时。"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type", "请求内容类型不受支持。"
	case http.StatusTooManyRequests:
		return "login_rate_limited", "登录尝试过于频繁，请稍后重试。"
	case http.StatusServiceUnavailable:
		return "service_unavailable", "服务暂时不可用。"
	default:
		return "internal_error", "服务暂时无法处理请求。"
	}
}
