package apihttp

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func clientAddress(request *http.Request, trusted []netip.Prefix) netip.Addr {
	peer, ok := parseRemoteAddress(request.RemoteAddr)
	if !ok || len(trusted) == 0 || !addressInPrefixes(peer, trusted) {
		return peer
	}
	forwarded := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
	if len(forwarded) == 1 && strings.TrimSpace(forwarded[0]) == "" {
		return peer
	}
	for index := len(forwarded) - 1; index >= 0; index-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(forwarded[index]))
		if err != nil {
			return peer
		}
		candidate = candidate.Unmap()
		if !addressInPrefixes(candidate, trusted) {
			return candidate
		}
	}
	return peer
}

func parseRemoteAddress(value string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(value), "[]")
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func addressInPrefixes(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
