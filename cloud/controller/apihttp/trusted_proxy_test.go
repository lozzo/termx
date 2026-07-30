package apihttp

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientAddressTrustBoundary(t *testing.T) {
	tests := []struct {
		name       string
		remote     string
		forwarded  string
		trusted    []netip.Prefix
		wantClient string
	}{
		{name: "empty trust ignores forwarded header", remote: "203.0.113.9:443", forwarded: "198.51.100.7", wantClient: "203.0.113.9"},
		{name: "untrusted peer cannot spoof XFF", remote: "203.0.113.9:443", forwarded: "198.51.100.7", trusted: prefixes("127.0.0.0/8"), wantClient: "203.0.113.9"},
		{name: "trusted IPv4 proxy accepts client", remote: "127.0.0.1:8444", forwarded: "198.51.100.7", trusted: prefixes("127.0.0.0/8"), wantClient: "198.51.100.7"},
		{name: "rightmost untrusted IPv6 wins across trusted hops", remote: "[::1]:8444", forwarded: "198.51.100.99, 2001:db8::7, 10.2.3.4, 127.0.0.2", trusted: prefixes("127.0.0.0/8", "::1/128", "10.0.0.0/8"), wantClient: "2001:db8::7"},
		{name: "invalid forwarded chain fails to peer", remote: "127.0.0.1:8444", forwarded: "198.51.100.7, invalid", trusted: prefixes("127.0.0.0/8"), wantClient: "127.0.0.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "https://cloud.example/api/account/login", nil)
			request.RemoteAddr = test.remote
			request.Header.Set("X-Forwarded-For", test.forwarded)
			if got := clientAddress(request, test.trusted); got.String() != test.wantClient {
				t.Fatalf("client address = %q, want %q", got, test.wantClient)
			}
		})
	}
}

func prefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}
