// Package apihttp 提供 Controller 浏览器 JSON、安装和静态管理页面 adapter。
// 它只组合 edgeconfig 与 Directory 投影，不持有第二份业务状态。
package apihttp

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/muxvia/muxvia/cloud/controller/directory"
	"github.com/muxvia/muxvia/cloud/controller/edgeconfig"
	"github.com/muxvia/muxvia/cloud/controller/enrollment"
	"github.com/muxvia/muxvia/cloud/controller/install"
	"github.com/muxvia/muxvia/cloud/securetransport"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:embed web/*
var webFiles embed.FS

// Config 是 Controller 原生 HTTPS 管理/安装 listener 的装配输入。
// R3 使用部署 secret 的 HTTP Basic 门禁；R7 会替换为账号 session/RBAC。
type Config struct {
	ListenAddress      string
	TLSCertificateFile string
	TLSPrivateKeyFile  string
	PublicOrigin       string
	OperatorUsername   string
	OperatorPassword   string
	Edges              *edgeconfig.Service
	Directory          *directory.Directory
	Install            *install.Service
	Enrollment         *enrollment.Service
}

// Server 拥有原生 HTTPS listener 生命周期，不拥有 Edge 配置或实时目录。
type Server struct {
	listener     net.Listener
	httpServer   *http.Server
	errors       chan error
	shutdownOnce sync.Once
}

// Start 验证 TLS/认证配置、绑定 listener 并启动 HTTPS。
func Start(config Config) (*Server, error) {
	config.ListenAddress = strings.TrimSpace(config.ListenAddress)
	if config.ListenAddress == "" || config.Edges == nil || config.Directory == nil || config.Install == nil || strings.TrimSpace(config.OperatorUsername) == "" || config.OperatorPassword == "" {
		return nil, errors.New("HTTP listen, services, and operator credentials are required")
	}
	tlsConfig, err := securetransport.NewServerTLSConfig(securetransport.ServerOptions{CertificateFile: config.TLSCertificateFile, PrivateKeyFile: config.TLSPrivateKeyFile})
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen Controller HTTPS: %w", err)
	}
	handler, err := NewHandler(config)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	server := &Server{listener: listener, errors: make(chan error, 1)}
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
	var result error
	server.shutdownOnce.Do(func() { result = server.httpServer.Shutdown(ctx) })
	return result
}

// NewHandler 构造可测试的 HTTP adapter；调用方仍必须在生产使用 TLS listener。
func NewHandler(config Config) (http.Handler, error) {
	config.PublicOrigin = strings.TrimRight(strings.TrimSpace(config.PublicOrigin), "/")
	config.OperatorUsername = strings.TrimSpace(config.OperatorUsername)
	if config.Edges == nil || config.Directory == nil || config.Install == nil || config.PublicOrigin == "" || config.OperatorUsername == "" || config.OperatorPassword == "" {
		return nil, errors.New("HTTP handler services, public origin, and credentials are required")
	}
	var grpcServer *grpc.Server
	if config.Enrollment != nil {
		grpcServer = grpc.NewServer()
		cloudv1.RegisterEnrollmentServiceServer(grpcServer, config.Enrollment)
	}
	handler := &handler{config: config, grpcServer: grpcServer}
	return handler, nil
}

type handler struct {
	config     Config
	grpcServer *grpc.Server
}

func (handler *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'")
	if handler.grpcServer != nil && request.ProtoMajor == 2 && strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/grpc") {
		handler.grpcServer.ServeHTTP(writer, request)
		return
	}
	switch {
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/install/edge/"):
		handler.installScript(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/artifacts/muxvia-cloud-edge-linux-amd64":
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write(handler.config.Install.Artifact())
	case request.Method == http.MethodPost && request.URL.Path == "/api/install/register":
		handler.register(writer, request)
	case strings.HasPrefix(request.URL.Path, "/api/operator/"):
		if !handler.authorize(writer, request) || !handler.allowMutationOrigin(writer, request) {
			return
		}
		handler.operator(writer, request)
	default:
		if !handler.authorize(writer, request) {
			return
		}
		handler.serveStatic(writer, request)
	}
}

func (handler *handler) serveStatic(writer http.ResponseWriter, request *http.Request) {
	var name, contentType string
	switch request.URL.Path {
	case "/", "/index.html", "/edges", "/daemons":
		name, contentType = "web/index.html", "text/html; charset=utf-8"
	case "/app.js":
		name, contentType = "web/app.js", "text/javascript; charset=utf-8"
	case "/styles.css":
		name, contentType = "web/styles.css", "text/css; charset=utf-8"
	default:
		http.NotFound(writer, request)
		return
	}
	payload, err := webFiles.ReadFile(name)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Cache-Control", "no-cache")
	_, _ = writer.Write(payload)
}

func (handler *handler) operator(writer http.ResponseWriter, request *http.Request) {
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
			response, err := handler.config.Enrollment.CreateEnrollment(request.Context(), input, "muxvia cloud enroll --controller "+handler.config.PublicOrigin)
			if err != nil {
				writeError(writer, http.StatusBadRequest, err)
				return
			}
			writeProto(writer, http.StatusCreated, response)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
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
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if request.Method == http.MethodPut && strings.HasPrefix(request.URL.Path, "/api/operator/edges/") {
		handler.updateEdge(writer, request)
		return
	}
	http.NotFound(writer, request)
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
		response.Edges = append(response.Edges, projectEdge(edge, runtimeByID[edge.ID]))
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
	response := &cloudv1.CreateEdgeResponse{Edge: projectEdge(edge, directory.EdgeProjection{}), InstallCommand: "curl -fsSL " + handler.config.PublicOrigin + "/install/edge/" + claim + " | sudo sh", ClaimExpiresAt: timestamppb.New(expiresAt)}
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
	edge, err := handler.config.Edges.UpdateEdge(request.Context(), edgeconfig.UpdateInput{EdgeID: input.GetEdgeId(), ExpectedRevision: input.GetExpectedRevision(), Name: input.GetName(), Region: input.GetRegion(), Capacity: input.GetCapacity(), PublicEndpoint: input.GetPublicEndpoint(), Enabled: input.GetEnabled()})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, edgeconfig.ErrRevisionConflict) {
			status = http.StatusConflict
		}
		writeError(writer, status, err)
		return
	}
	runtimeProjection, _, _ := handler.config.Directory.Edge(request.Context(), edge.ID)
	writeProto(writer, http.StatusOK, &cloudv1.UpdateEdgeResponse{Edge: projectEdge(edge, runtimeProjection)})
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

func (handler *handler) authorize(writer http.ResponseWriter, request *http.Request) bool {
	username, password, ok := request.BasicAuth()
	validUser := subtle.ConstantTimeCompare([]byte(username), []byte(handler.config.OperatorUsername)) == 1
	validPassword := subtle.ConstantTimeCompare([]byte(password), []byte(handler.config.OperatorPassword)) == 1
	if !ok || !validUser || !validPassword {
		writer.Header().Set("WWW-Authenticate", `Basic realm="Muxvia Cloud R3", charset="UTF-8"`)
		writer.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
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
	return true
}

func projectEdge(edge edgeconfig.Edge, runtime directory.EdgeProjection) *cloudv1.ManagedEdge {
	projection := &cloudv1.ManagedEdge{Config: &cloudv1.EdgeDesiredConfig{EdgeId: edge.ID, Version: edge.ConfigVersion, Name: edge.Name, Region: edge.Region, Capacity: edge.Capacity, PublicEndpoint: edge.PublicEndpoint, Enabled: edge.Enabled}, ConfigRevision: edge.Revision, Runtime: &cloudv1.EdgeRuntimeProjection{}}
	if runtime.EdgeID != "" {
		projection.Runtime = &cloudv1.EdgeRuntimeProjection{Online: true, BootId: runtime.BootID, ConnectionId: runtime.ConnectionID, SoftwareVersion: runtime.SoftwareVersion, RuntimeRevision: runtime.RuntimeRevision, AgentCount: uint64(runtime.AgentCount), SessionCount: uint64(runtime.SessionCount), ConnectedAt: timestamppb.New(runtime.ConnectedAt), LastHeartbeat: timestamppb.New(runtime.LastHeartbeat)}
	}
	return projection
}

func readProto(request *http.Request, message proto.Message) error {
	defer request.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		return err
	}
	return (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, message)
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
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
}
