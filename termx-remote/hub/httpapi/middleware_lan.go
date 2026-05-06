package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

var privateNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8", "::1/128",
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"fd00::/8", "fe80::/10",
	} {
		_, n, _ := net.ParseCIDR(cidr)
		privateNets = append(privateNets, n)
	}
}

func ParseLANIPs(ips []string) ([]*net.IPNet, error) {
	var result []*net.IPNet
	for _, s := range ips {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			_, n, err := net.ParseCIDR(s)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", s, err)
			}
			result = append(result, n)
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP %q", s)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		result = append(result, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return result, nil
}

// NewLANFilter returns HTTP middleware that filters requests by remote IP.
func NewLANFilter(allowLAN bool, allowedNets []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip := net.ParseIP(host)
			if ip == nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !allowLAN {
				if !ip.IsLoopback() {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			nets := allowedNets
			if len(nets) == 0 {
				nets = privateNets
			}
			for _, n := range nets {
				if n.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}
