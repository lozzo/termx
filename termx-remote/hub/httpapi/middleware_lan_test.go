package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseLANIPs(t *testing.T) {
	nets, err := ParseLANIPs([]string{"192.168.1.0/24", "10.0.0.1", " ::1 "})
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 3 {
		t.Fatalf("nets length = %d, want 3", len(nets))
	}
	if !nets[0].Contains(parseIP(t, "192.168.1.50")) {
		t.Fatal("expected CIDR to contain LAN IP")
	}
	if !nets[1].Contains(parseIP(t, "10.0.0.1")) || nets[1].Contains(parseIP(t, "10.0.0.2")) {
		t.Fatal("expected bare IPv4 to be parsed as single-host CIDR")
	}
	if !nets[2].Contains(parseIP(t, "::1")) {
		t.Fatal("expected bare IPv6 to be parsed as single-host CIDR")
	}
}

func TestParseLANIPsRejectsInvalidInput(t *testing.T) {
	if _, err := ParseLANIPs([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected invalid IP error")
	}
	if _, err := ParseLANIPs([]string{"192.168.1.0/33"}); err == nil {
		t.Fatal("expected invalid CIDR error")
	}
}

func TestLANFilterLoopbackOnly(t *testing.T) {
	handler := NewLANFilter(false, nil)(okHandler())

	if code := serveWithRemoteAddr(handler, "127.0.0.1:1234"); code != http.StatusNoContent {
		t.Fatalf("loopback status = %d", code)
	}
	if code := serveWithRemoteAddr(handler, "192.168.1.5:1234"); code != http.StatusForbidden {
		t.Fatalf("LAN status = %d", code)
	}
}

func TestLANFilterPrivateNetworks(t *testing.T) {
	handler := NewLANFilter(true, nil)(okHandler())

	for _, addr := range []string{"10.1.2.3:1234", "172.16.1.2:1234", "192.168.1.5:1234", "[::1]:1234", "[fd00::1]:1234"} {
		if code := serveWithRemoteAddr(handler, addr); code != http.StatusNoContent {
			t.Fatalf("%s status = %d", addr, code)
		}
	}
	if code := serveWithRemoteAddr(handler, "8.8.8.8:1234"); code != http.StatusForbidden {
		t.Fatalf("public status = %d", code)
	}
}

func TestLANFilterAllowedNets(t *testing.T) {
	nets, err := ParseLANIPs([]string{"192.168.1.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewLANFilter(true, nets)(okHandler())

	if code := serveWithRemoteAddr(handler, "192.168.1.5:1234"); code != http.StatusNoContent {
		t.Fatalf("allowed status = %d", code)
	}
	if code := serveWithRemoteAddr(handler, "192.168.2.5:1234"); code != http.StatusForbidden {
		t.Fatalf("blocked status = %d", code)
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func serveWithRemoteAddr(handler http.Handler, remoteAddr string) int {
	req := httptest.NewRequest(http.MethodGet, "http://termx.test/", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

func parseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("parse IP %q", s)
	}
	return ip
}
