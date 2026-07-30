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
	forwardedFields := request.Header.Values("X-Forwarded-For")
	if len(forwardedFields) == 0 {
		return peer
	}
	forwarded := make([]netip.Addr, 0, len(forwardedFields))
	for _, field := range forwardedFields {
		for _, value := range strings.Split(field, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				return peer
			}
			candidate, err := netip.ParseAddr(value)
			if err != nil {
				return peer
			}
			forwarded = append(forwarded, candidate.Unmap())
		}
	}
	for index := len(forwarded) - 1; index >= 0; index-- {
		candidate := forwarded[index]
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
