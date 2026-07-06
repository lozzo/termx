package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/connection"
	sshtransport "github.com/lozzow/termx/termx-shared/transport/ssh"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// TestSSHEndpointCreateAttachIntegration 是显式开启的真实 SSH transport harness。
// 它验证 TUI services 实际依赖的 EndpointManager -> SSH stdio-proxy -> protocol adapter 链路能在远端 daemon 上 create、attach、list；
// 默认跳过，避免普通准入依赖外部服务器、known_hosts 或远端 termx 版本。
func TestSSHEndpointCreateAttachIntegration(t *testing.T) {
	targets := sshEndpointIntegrationTargets()
	if len(targets) == 0 {
		t.Skip("set TERMX_TEST_SSH_ENDPOINTS=root@host[,root@host2] to run real SSH endpoint create/attach harness")
	}
	for _, target := range targets {
		target := target
		t.Run(sshEndpointTestName(target), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			endpointID := state.EndpointID(sshEndpointTestName(target))
			registry := connection.Registry{
				Version: 1,
				Default: connection.EndpointID(endpointID),
				Connections: map[connection.EndpointID]connection.Config{
					connection.EndpointID(endpointID): {
						ID:           connection.EndpointID(endpointID),
						Label:        "SSH Harness " + target,
						Transport:    connection.TransportSSH,
						Address:      target,
						ConnectMode:  connection.ConnectOnDemand,
						Enabled:      true,
						RemoteSocket: "auto",
					},
				},
			}
			manager := NewEndpointManagerWithDialers(registry, map[connection.TransportKind]EndpointDialer{
				connection.TransportSSH: func(ctx context.Context, cfg connection.Config) (EndpointServiceBundle, error) {
					transport, err := sshtransport.Dial(ctx, sshtransport.DialOptions{
						Address:      cfg.Address,
						AuthRef:      cfg.AuthRef,
						RemoteSocket: cfg.RemoteSocket,
					})
					if err != nil {
						return EndpointServiceBundle{}, fmt.Errorf("ssh endpoint %q dial: %w", cfg.ID, err)
					}
					client := protocol.NewClient(transport)
					t.Cleanup(func() { _ = client.Close() })
					if err := client.Hello(ctx, protocol.Hello{Version: wire.Version, Client: "termx-tui-v3-test:ssh:" + string(cfg.ID)}); err != nil {
						_ = client.Close()
						return EndpointServiceBundle{}, fmt.Errorf("ssh endpoint %q hello: %w", cfg.ID, err)
					}
					terminal := ProtocolTerminalServiceAdapter{Client: client}
					return EndpointServiceBundle{
						EndpointID: state.EndpointID(cfg.ID),
						Terminal:   terminal,
						Surface:    terminal,
						LiveEvents: terminal,
						Path:       ProtocolPathServiceAdapter{Client: client},
						Core:       ProtocolCoreClientAdapter{Client: client},
					}, nil
				},
			})
			terminalID := fmt.Sprintf("termx-ssh-harness-%s-%d", strings.ReplaceAll(sshEndpointTestName(target), "-", ""), time.Now().UnixNano())

			created, err := manager.Create(ctx, TerminalCreateRequest{
				EndpointID: endpointID,
				TerminalID: terminalID,
				Title:      "ssh-harness-" + sshEndpointTestName(target),
				Command:    []string{"/bin/sh", "-lc", "printf termx-ssh-harness-ready; sleep 20"},
				Cols:       80,
				Rows:       24,
			})
			if err != nil {
				t.Fatalf("remote create %s: %v", target, err)
			}
			if created.EndpointID != endpointID || created.TerminalID != terminalID {
				t.Fatalf("remote create should preserve endpoint ref, created=%#v endpoint=%s terminal=%s", created, endpointID, terminalID)
			}
			defer func() {
				if err := manager.Remove(context.Background(), TerminalRemoveRequest{EndpointID: endpointID, TerminalID: terminalID}); err != nil {
					t.Logf("cleanup remove %s on %s: %v", terminalID, target, err)
				}
			}()

			attached, err := manager.Attach(ctx, TerminalAttachRequest{
				EndpointID:   endpointID,
				TerminalID:   terminalID,
				Cols:         80,
				Rows:         24,
				Mode:         "collaborator",
				ResizePolicy: state.TerminalResizeRoleOwner,
				SurfaceID:    "ssh-harness-surface",
				ViewID:       "ssh-harness-view",
			})
			if err != nil {
				t.Fatalf("remote attach %s terminal %s: %v", target, terminalID, err)
			}
			if attached.EndpointID != endpointID || attached.TerminalID != terminalID || attached.Channel == 0 {
				t.Fatalf("remote attach should return endpoint-scoped channel, attached=%#v endpoint=%s terminal=%s", attached, endpointID, terminalID)
			}

			list, err := manager.List(ctx, TerminalListRequest{EndpointID: endpointID})
			if err != nil {
				t.Fatalf("remote list %s after create: %v", target, err)
			}
			if !terminalListContains(list.Items, endpointID, terminalID) {
				t.Fatalf("remote list %s should include created terminal %s, items=%#v", target, terminalID, list.Items)
			}
		})
	}
}

func sshEndpointIntegrationTargets() []string {
	raw := strings.TrimSpace(os.Getenv("TERMX_TEST_SSH_ENDPOINTS"))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })
	targets := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			targets = append(targets, part)
		}
	}
	return targets
}

func sshEndpointTestName(target string) string {
	name := strings.NewReplacer("@", "-", ".", "-", ":", "-").Replace(strings.TrimSpace(target))
	name = strings.Trim(name, "-")
	if name == "" {
		return fmt.Sprintf("endpoint-%d", time.Now().UnixNano())
	}
	return name
}

func terminalListContains(items []TerminalPoolItem, endpointID state.EndpointID, terminalID string) bool {
	for _, item := range items {
		if item.EndpointID == endpointID && item.TerminalID == terminalID {
			return true
		}
	}
	return false
}
