package webrtc

import (
	"testing"

	pion "github.com/pion/webrtc/v4"
)

func TestContainsLoopbackTURNOnlyAcceptsExplicitLoopbackRelay(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "ipv4", url: "turn:127.0.0.1:3478?transport=udp", want: true},
		{name: "localhost", url: "turn:localhost:3478?transport=udp", want: true},
		{name: "public relay", url: "turn:relay.example.test:3478?transport=udp", want: false},
		{name: "loopback stun", url: "stun:127.0.0.1:3478", want: false},
		{name: "malformed", url: "turn:not-an-address", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := containsLoopbackTURN([]pion.ICEServer{{URLs: []string{test.url}}})
			if got != test.want {
				t.Fatalf("containsLoopbackTURN(%q) = %t, want %t", test.url, got, test.want)
			}
		})
	}
}
