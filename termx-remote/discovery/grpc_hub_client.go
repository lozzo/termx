package discovery

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
)

type GRPCHubClient struct {
	conn   *grpc.ClientConn
	client pb.AgentHubClient
	token  string
}

// NewGRPCHubClient establishes a gRPC connection. https:// URLs use TLS;
// all other targets use plaintext transport credentials.
func NewGRPCHubClient(hubURL, token string) (*GRPCHubClient, error) {
	target, tlsEnabled, err := grpcTarget(hubURL)
	if err != nil {
		return nil, err
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if tlsEnabled {
		opts = []grpc.DialOption{
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})),
		}
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", target, err)
	}
	return &GRPCHubClient{conn: conn, client: pb.NewAgentHubClient(conn), token: token}, nil
}

func (c *GRPCHubClient) Connect(ctx context.Context) (pb.AgentHub_ConnectClient, error) {
	md := metadata.Pairs("authorization", "Bearer "+c.token)
	return c.client.Connect(metadata.NewOutgoingContext(ctx, md))
}

func (c *GRPCHubClient) Close() error {
	return c.conn.Close()
}

func grpcTarget(raw string) (target string, tlsEnabled bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, fmt.Errorf("hub url is required")
	}
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", false, fmt.Errorf("parse hub url: %w", err)
		}
		if u.Host == "" {
			return "", false, fmt.Errorf("hub url host is required")
		}
		return u.Host, u.Scheme == "https", nil
	}
	return raw, false, nil
}
