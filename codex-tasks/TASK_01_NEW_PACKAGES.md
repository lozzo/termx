# TASK_01 — 新增独立包

**Wave**: 1（无依赖，可与 TASK_02/03/04 同时执行）  
**验证**: `cd termx-remote && go test -race ./session/token/... ./identity/... && go build ./discovery/... ./hub/httpapi/...`

---

## 1. 新建 `termx-remote/session/token/token.go`

```go
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const tokenVersion = "termx-session-v1:"

type Claims struct {
	SessionID    string   `json:"sid"`
	MachineID    string   `json:"mid"`
	Capabilities []string `json:"cap"`
	IssuedAt     int64    `json:"iat"`
	ExpiresAt    int64    `json:"exp"`
}

func Issue(machineSecret []byte, claims Claims) (string, error) {
	if len(machineSecret) < 32 {
		return "", errors.New("machine secret must be at least 32 bytes")
	}
	caps := append([]string(nil), claims.Capabilities...)
	sort.Strings(caps)
	claims.Capabilities = caps
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal token claims: %w", err)
	}
	p := base64.RawURLEncoding.EncodeToString(payload)
	m := base64.RawURLEncoding.EncodeToString(computeMAC(machineSecret, p))
	return p + "." + m, nil
}

func Verify(tok string, machineSecret []byte, now time.Time) (Claims, error) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Claims{}, errors.New("invalid token format")
	}
	expected := computeMAC(machineSecret, parts[0])
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("decode mac: %w", err)
	}
	if !hmac.Equal(expected, provided) {
		return Claims{}, errors.New("token signature invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, fmt.Errorf("unmarshal claims: %w", err)
	}
	if now.Unix() >= claims.ExpiresAt {
		return Claims{}, errors.New("token expired")
	}
	return claims, nil
}

func computeMAC(secret []byte, msg string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(tokenVersion))
	h.Write([]byte(msg))
	return h.Sum(nil)
}
```

## 2. 新建 `termx-remote/session/token/token_test.go`

```go
package token_test

import (
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/session/token"
)

var secret = make([]byte, 32)

func baseClaims() token.Claims {
	return token.Claims{
		SessionID:    "sid1",
		MachineID:    "mid1",
		Capabilities: []string{"terminal"},
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}
}

func TestIssueAndVerify(t *testing.T) {
	tok, err := token.Issue(secret, baseClaims())
	if err != nil { t.Fatal(err) }
	got, err := token.Verify(tok, secret, time.Now())
	if err != nil { t.Fatal(err) }
	if got.MachineID != "mid1" { t.Fatalf("machine_id: %s", got.MachineID) }
}

func TestVerifyExpired(t *testing.T) {
	c := baseClaims()
	c.ExpiresAt = time.Now().Add(-time.Hour).Unix()
	tok, _ := token.Issue(secret, c)
	if _, err := token.Verify(tok, secret, time.Now()); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestVerifyTamperedPayload(t *testing.T) {
	tok, _ := token.Issue(secret, baseClaims())
	parts := strings.SplitN(tok, ".", 2)
	if _, err := token.Verify("dGFtcGVyZWQ."+parts[1], secret, time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	tok, _ := token.Issue(secret, baseClaims())
	wrong := make([]byte, 32); wrong[0] = 0xFF
	if _, err := token.Verify(tok, wrong, time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCapabilitiesSorted(t *testing.T) {
	c := baseClaims()
	c.Capabilities = []string{"terminal_management", "file_manager", "terminal"}
	tok, _ := token.Issue(secret, c)
	got, _ := token.Verify(tok, secret, time.Now())
	for i := 1; i < len(got.Capabilities); i++ {
		if got.Capabilities[i] < got.Capabilities[i-1] {
			t.Fatal("capabilities not sorted")
		}
	}
}
```

---

## 3. 新建 `termx-remote/identity/machine_secret.go`

```go
package identity

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MachineSecretFilename = "machine_secret"

func LoadOrCreateMachineSecret(dir string) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dir, MachineSecretFilename)
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("machine secret wrong length %d", len(data))
		}
		_ = os.Chmod(path, 0o600)
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read machine secret: %w", err)
	}
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	if err := persistSecret(path, secret); err != nil {
		if errors.Is(err, os.ErrExist) {
			data, _ := os.ReadFile(path)
			return data, nil
		}
		return nil, err
	}
	return secret, nil
}

func persistSecret(path string, secret []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	_, we := f.Write(secret)
	ce := f.Chmod(0o600)
	cl := f.Close()
	if we != nil { os.Remove(tmp); return fmt.Errorf("write: %w", we) }
	if ce != nil { os.Remove(tmp); return fmt.Errorf("chmod: %w", ce) }
	if cl != nil { os.Remove(tmp); return fmt.Errorf("close: %w", cl) }
	if err := os.Link(tmp, path); err != nil {
		os.Remove(tmp)
		if os.IsExist(err) { return fmt.Errorf("%w", os.ErrExist) }
		return fmt.Errorf("link: %w", err)
	}
	os.Remove(tmp)
	os.Chmod(path, 0o600)
	return nil
}
```

## 4. 新建 `termx-remote/identity/machine_secret_test.go`

```go
package identity_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lozzow/termx/termx-remote/identity"
)

func TestLoadOrCreateNew(t *testing.T) {
	s, err := identity.LoadOrCreateMachineSecret(t.TempDir())
	if err != nil { t.Fatal(err) }
	if len(s) != 32 { t.Fatalf("want 32 bytes, got %d", len(s)) }
}

func TestLoadOrCreateExisting(t *testing.T) {
	dir := t.TempDir()
	s1, _ := identity.LoadOrCreateMachineSecret(dir)
	s2, err := identity.LoadOrCreateMachineSecret(dir)
	if err != nil { t.Fatal(err) }
	if string(s1) != string(s2) { t.Fatal("different secret on reload") }
}

func TestConcurrentCreate(t *testing.T) {
	dir := t.TempDir()
	errs := make([]error, 10)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, errs[i] = identity.LoadOrCreateMachineSecret(dir) }(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil { t.Fatalf("goroutine %d: %v", i, e) }
	}
}

func TestPermissions(t *testing.T) {
	dir := t.TempDir()
	identity.LoadOrCreateMachineSecret(dir)
	info, _ := os.Stat(filepath.Join(dir, identity.MachineSecretFilename))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("want 0600, got %o", info.Mode().Perm())
	}
}
```

---

## 5. 新建 `termx-remote/discovery/latency.go`

```go
package discovery

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"
)

type HubProbeResult struct {
	URL       string
	Latency   time.Duration
	Available bool
}

func ProbeHub(ctx context.Context, hubURL string, timeout time.Duration) HubProbeResult {
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodHead, hubURL+"/api/health", nil)
	if err != nil {
		return HubProbeResult{URL: hubURL, Latency: -1}
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return HubProbeResult{URL: hubURL, Latency: -1}
	}
	resp.Body.Close()
	return HubProbeResult{URL: hubURL, Latency: elapsed, Available: true}
}

// ProbeHubs 并发探测，每个 hub 探测 probeCount 次取中位数，按延迟升序返回。
func ProbeHubs(ctx context.Context, urls []string, timeout time.Duration, probeCount int) []HubProbeResult {
	if probeCount <= 0 {
		probeCount = 1
	}
	results := make([]HubProbeResult, len(urls))
	var wg sync.WaitGroup
	for i, url := range urls {
		wg.Add(1)
		go func(i int, url string) {
			defer wg.Done()
			var lats []time.Duration
			for j := 0; j < probeCount; j++ {
				r := ProbeHub(ctx, url, timeout)
				if r.Available {
					lats = append(lats, r.Latency)
				}
			}
			if len(lats) == 0 {
				results[i] = HubProbeResult{URL: url, Latency: -1}
				return
			}
			sort.Slice(lats, func(a, b int) bool { return lats[a] < lats[b] })
			results[i] = HubProbeResult{URL: url, Latency: lats[len(lats)/2], Available: true}
		}(i, url)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool {
		if !results[i].Available { return false }
		if !results[j].Available { return true }
		return results[i].Latency < results[j].Latency
	})
	return results
}
```

---

## 6. 新建 `termx-remote/hub/httpapi/middleware_lan.go`

```go
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
		if s == "" { continue }
		if strings.Contains(s, "/") {
			_, n, err := net.ParseCIDR(s)
			if err != nil { return nil, fmt.Errorf("invalid CIDR %q: %w", s, err) }
			result = append(result, n)
		} else {
			ip := net.ParseIP(s)
			if ip == nil { return nil, fmt.Errorf("invalid IP %q", s) }
			bits := 32
			if ip.To4() == nil { bits = 128 }
			result = append(result, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return result, nil
}

// NewLANFilter 返回 HTTP middleware 过滤来源 IP。
// allowLAN=false: 仅 loopback
// allowLAN=true, allowedNets 空: 允许所有私有 IP
// allowLAN=true, allowedNets 非空: 只允许白名单
func NewLANFilter(allowLAN bool, allowedNets []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil { host = r.RemoteAddr }
			ip := net.ParseIP(host)
			if ip == nil { http.Error(w, "forbidden", http.StatusForbidden); return }
			if !allowLAN {
				if !ip.IsLoopback() { http.Error(w, "forbidden", http.StatusForbidden); return }
				next.ServeHTTP(w, r); return
			}
			nets := allowedNets
			if len(nets) == 0 { nets = privateNets }
			for _, n := range nets {
				if n.Contains(ip) { next.ServeHTTP(w, r); return }
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}
```
