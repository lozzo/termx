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
		forwarded  []string
		trusted    []netip.Prefix
		wantClient string
	}{
		{name: "empty trust ignores forwarded fields", remote: "203.0.113.9:443", forwarded: []string{"203.0.113.250", "198.51.100.7"}, wantClient: "203.0.113.9"},
		{name: "untrusted peer cannot spoof XFF", remote: "203.0.113.9:443", forwarded: []string{"198.51.100.7"}, trusted: prefixes("127.0.0.0/8"), wantClient: "203.0.113.9"},
		{name: "trusted IPv4 proxy accepts client", remote: "127.0.0.1:8444", forwarded: []string{"198.51.100.7"}, trusted: prefixes("127.0.0.0/8"), wantClient: "198.51.100.7"},
		{name: "single nginx appended field ignores forged left value", remote: "127.0.0.1:8444", forwarded: []string{"203.0.113.250, 198.51.100.7"}, trusted: prefixes("127.0.0.0/8"), wantClient: "198.51.100.7"},
		{name: "repeated fields include trusted appended value", remote: "127.0.0.1:8444", forwarded: []string{"203.0.113.250", "198.51.100.7"}, trusted: prefixes("127.0.0.0/8"), wantClient: "198.51.100.7"},
		{name: "rightmost untrusted IPv6 wins across repeated trusted hops", remote: "[::1]:8444", forwarded: []string{"198.51.100.99, 2001:db8::7", "10.2.3.4, 127.0.0.2"}, trusted: prefixes("127.0.0.0/8", "::1/128", "10.0.0.0/8"), wantClient: "2001:db8::7"},
		{name: "invalid left field fails closed before selecting client", remote: "127.0.0.1:8444", forwarded: []string{"invalid", "198.51.100.7"}, trusted: prefixes("127.0.0.0/8"), wantClient: "127.0.0.1"},
		{name: "empty repeated field fails closed", remote: "127.0.0.1:8444", forwarded: []string{"198.51.100.7", ""}, trusted: prefixes("127.0.0.0/8"), wantClient: "127.0.0.1"},
		{name: "empty comma boundary fails closed", remote: "127.0.0.1:8444", forwarded: []string{"198.51.100.7,, 127.0.0.2"}, trusted: prefixes("127.0.0.0/8"), wantClient: "127.0.0.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "https://cloud.example/api/account/login", nil)
			request.RemoteAddr = test.remote
			for _, forwarded := range test.forwarded {
				request.Header.Add("X-Forwarded-For", forwarded)
			}
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
