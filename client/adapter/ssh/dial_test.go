package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"

	"github.com/lozzow/termx/client/endpoint"
	golangssh "golang.org/x/crypto/ssh"
)

func TestSSHRouteProjectionKeepsUserHostAndTCPCandidateExplicit(t *testing.T) {
	route := endpoint.AccessRoute{Host: "build@studio.example", Port: 2222}
	user, host := sshUserAndHost(route)
	if user != "build" || host != "studio.example" {
		t.Fatalf("SSH target = %q@%q", user, host)
	}
	candidate := "candidate:1 1 tcp 1671430143 127.0.0.1 41121 typ host tcptype passive"
	if got := projectTCPCandidate(candidate, "127.0.0.1", 54321); got != "candidate:1 1 tcp 1671430143 127.0.0.1 54321 typ host tcptype passive" {
		t.Fatalf("projected candidate = %q", got)
	}
	udp := "candidate:2 1 udp 1 127.0.0.1 41121 typ host"
	if got := projectTCPCandidate(udp, "127.0.0.1", 54321); got != udp {
		t.Fatalf("UDP candidate changed to %q", got)
	}
}

func TestPinnedHostKeyCallbackRejectsUnpinnedKey(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := golangssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := golangssh.FingerprintSHA256(signer.PublicKey())
	if err := pinnedHostKeyCallback([]string{fingerprint})("host", &net.TCPAddr{}, signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := pinnedHostKeyCallback([]string{"SHA256:not-the-key"})("host", &net.TCPAddr{}, signer.PublicKey()); err == nil {
		t.Fatal("unpinned host key unexpectedly accepted")
	}
}
