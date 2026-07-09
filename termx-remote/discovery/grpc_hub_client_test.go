package discovery

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
)

func TestGRPCHubClientConnectSendsBearerToken(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	pb.RegisterAgentHubServer(server, &authCaptureServer{wantAuth: "Bearer relay-token"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	defer func() {
		server.Stop()
		<-done
	}()

	client, err := NewGRPCHubClient("http://"+listener.Addr().String(), "relay-token")
	if err != nil {
		t.Fatalf("new grpc hub client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := stream.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Register{
		Register: &pb.RegisterRequest{AgentId: "agent-1"},
	}}); err != nil {
		t.Fatalf("send register: %v", err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv ack: %v", err)
	}
	if msg.GetRegisterAck().GetAgentSessionId() != "session-1" {
		t.Fatalf("ack = %+v", msg.GetRegisterAck())
	}
}

func TestGRPCTargetParsesURLSchemes(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		target  string
		wantTLS bool
	}{
		{name: "https", raw: "https://hub.example.test:443/path", target: "hub.example.test:443", wantTLS: true},
		{name: "http", raw: "http://127.0.0.1:8080", target: "127.0.0.1:8080"},
		{name: "target", raw: "dns:///hub.example.test:443", target: "dns:///hub.example.test:443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, tlsEnabled, err := grpcTarget(tt.raw)
			if err != nil {
				t.Fatalf("grpcTarget: %v", err)
			}
			if target != tt.target || tlsEnabled != tt.wantTLS {
				t.Fatalf("grpcTarget(%q) = (%q, %v), want (%q, %v)", tt.raw, target, tlsEnabled, tt.target, tt.wantTLS)
			}
		})
	}
}

type authCaptureServer struct {
	pb.UnimplementedAgentHubServer
	wantAuth string
}

func (s *authCaptureServer) Connect(stream pb.AgentHub_ConnectServer) error {
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok || len(md.Get("authorization")) == 0 || md.Get("authorization")[0] != s.wantAuth {
		return status.Error(codes.Unauthenticated, "authorization header missing")
	}
	if _, err := stream.Recv(); err != nil {
		return err
	}
	return stream.Send(&pb.HubToAgent{Payload: &pb.HubToAgent_RegisterAck{
		RegisterAck: &pb.RegisterResponse{AgentSessionId: "session-1"},
	}})
}
