package ice

import (
	"fmt"
	"strings"
	"time"
)

const (
	PathCloud     = "cloud"
	PathPublicP2P = "public_p2p"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type Lease struct {
	ID         string
	Path       string
	AllowRelay bool
	ExpiresAt  time.Time
}

type ICEServer struct {
	URLs       []string   `json:"urls"`
	Username   string     `json:"username,omitempty"`
	Credential string     `json:"credential,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type RTCConfig struct {
	Path       string      `json:"path"`
	ICEServers []ICEServer `json:"ice_servers"`
}

func (c RTCConfig) String() string {
	parts := []string{c.Path}
	for _, server := range c.ICEServers {
		parts = append(parts, server.URLs...)
	}
	return strings.Join(parts, " ")
}

func (c RTCConfig) GoString() string {
	return fmt.Sprintf("RTCConfig{Path:%q, ICEServers:%v}", c.Path, c.ICEServers)
}
