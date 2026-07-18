package ssh

import (
	"slices"
	"testing"

	"github.com/lozzow/termx/client/endpoint"
)

func TestSSHAttemptArgumentsComeOnlyFromSelectedRoute(t *testing.T) {
	route := endpoint.AccessRoute{
		ID: "ssh", Kind: endpoint.RouteSSHStdio, Enabled: true, Host: "studio.example", User: "build", Port: 2222,
		ProxyJump: "bastion", RemoteSocket: "/run/user/1000/termx.sock", Source: endpoint.SourceManual, PolicySource: endpoint.SourceUser,
	}
	if got := sshAddress(route); got != "build@studio.example" {
		t.Fatalf("SSH address = %q", got)
	}
	if got := sshArgs(route, []string{"-o", "LogLevel=ERROR"}); !slices.Equal(got, []string{"-o", "LogLevel=ERROR", "-p", "2222", "-J", "bastion"}) {
		t.Fatalf("SSH args = %v", got)
	}
	route.CredentialRef = "ssh:studio"
	if got := sshAddress(route); got != "build@studio.example" {
		t.Fatalf("credential ref changed route target snapshot: %q", got)
	}
}
