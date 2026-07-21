package webtest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apilayer "github.com/muxvia/muxvia/api_layer"
	core "github.com/muxvia/muxvia/core"
	"github.com/muxvia/muxvia/proto/bindingpb"
	"github.com/muxvia/muxvia/proto/cloudpb"
	remotev2daemon "github.com/muxvia/muxvia/remote/daemon"
	remotev2webrtc "github.com/muxvia/muxvia/remote/webrtc"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

func TestBrowserWASMCompletesDTLSBoundAuthHelloAPIAndGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("real Chrome/WebRTC E2E is disabled in short mode")
	}
	chrome := chromeExecutable(t)
	repoRoot := repositoryRoot(t)
	assets := t.TempDir()
	runCommand(t, repoRoot, filepath.Join(repoRoot, "scripts/build-web-client-wasm.sh"), assets)
	runCommand(t, repoRoot, filepath.Join(repoRoot, "node_modules/.bin/esbuild"),
		"clients/ui/test/e2e/webWasmSpike.ts", "--bundle", "--format=esm", "--platform=browser", "--target=chrome120",
		"--outfile="+filepath.Join(assets, "spike.js"))

	now := time.Now().UTC()
	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("device-web-spike", daemonPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	accessStore, err := remoteauth.LoadAccessStore(t.TempDir(), identity, remoteauth.AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer accessStore.Close()
	bundle, _, err := accessStore.IssuePairingBundle(remoteauth.PairingIssueOptions{
		Scope: remoteauth.Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	pairingPayload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core:     core.NewServer(core.WithApplicationExecutorFactory(apilayer.CoreApplicationExecutorFactory)),
		Identity: identity, AccessStore: accessStore, Now: func() time.Time { return now },
	}}

	resultCh := make(chan map[string]any, 1)
	var stageMu sync.Mutex
	stages := make([]string, 0, 16)
	var signalingID atomic.Uint64
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assets))))
	mux.HandleFunc("GET /", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("content-type", "text/html; charset=utf-8")
		fmt.Fprintf(response, `<!doctype html><html><head><meta charset="utf-8"><script>globalThis.muxviaDeviceFingerprint=%s</script><script type="module" src="/assets/spike.js"></script></head><body>running</body></html>`, mustJSON(identity.Fingerprint))
	})
	mux.HandleFunc("POST /grant", func(response http.ResponseWriter, request *http.Request) {
		record := &bindingpb.CredentialRecord{}
		if !decodeProto(response, request, record) {
			return
		}
		publicKey := ed25519.PublicKey(append([]byte(nil), record.GetPublicKey()...))
		if len(publicKey) != ed25519.PublicKeySize || remoteauth.Fingerprint(publicKey) != record.GetKeyFingerprint() {
			http.Error(response, "browser credential fingerprint mismatch", http.StatusBadRequest)
			return
		}
		exchanged, err := accessStore.RedeemPairingBundle(pairingPayload, publicKey, "muxvia-web-spike", now)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		record.CapabilityGrant = exchanged.Grant
		writeProto(response, record)
	})
	mux.HandleFunc("POST /resolve", func(response http.ResponseWriter, request *http.Request) {
		input := &cloudpb.ResolveEndpointRequest{}
		if !decodeProto(response, request, input) {
			return
		}
		writeProto(response, &cloudpb.ResolvedEndpoint{
			EndpointId: input.GetEndpointId(), TargetDeviceId: input.GetTargetDeviceId(), ManagedSessionId: "web-managed-session",
		})
	})
	mux.HandleFunc("POST /signal", func(response http.ResponseWriter, request *http.Request) {
		input := &cloudpb.CreateSignalingSessionRequest{}
		if !decodeProto(response, request, input) {
			return
		}
		id := fmt.Sprintf("web-signal-%d", signalingID.Add(1))
		answer, err := answerer.Answer(serverCtx, &cloudpb.SignalingOffer{
			SignalingSessionId: id, ManagedSessionId: input.GetManagedSessionId(), TargetDeviceId: input.GetTargetDeviceId(),
			Sdp: input.GetOfferSdp(), Candidates: input.GetCandidates(), RoutePreference: input.GetRoutePreference(), RelayOnly: input.GetRelayOnly(),
		}, nil)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		writeProto(response, &bindingpb.SignalingEvents{Events: []*cloudpb.SignalingEvent{{Payload: &cloudpb.SignalingEvent_Answer{Answer: answer}}}})
	})
	mux.HandleFunc("POST /result", func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var result map[string]any
		if err := json.NewDecoder(io.LimitReader(request.Body, 64<<10)).Decode(&result); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case resultCh <- result:
		default:
		}
		response.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /stage", func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(request.Body, 1024))
		stageMu.Lock()
		stages = append(stages, string(payload))
		stageMu.Unlock()
		response.WriteHeader(http.StatusNoContent)
	})
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	profile := t.TempDir()
	command := exec.Command(chrome,
		"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage", "--disable-background-networking",
		"--user-data-dir="+profile, testServer.URL,
	)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- command.Wait() }()
	select {
	case result := <-resultCh:
		if ok, _ := result["ok"].(bool); !ok {
			_ = command.Process.Kill()
			<-waitCh
			stageMu.Lock()
			observedStages := append([]string(nil), stages...)
			stageMu.Unlock()
			t.Fatalf("browser spike failed at stages %v: %v\nchrome: %s", observedStages, result, output.String())
		}
		first := strings.TrimSpace(fmt.Sprint(result["firstGeneration"]))
		second := strings.TrimSpace(fmt.Sprint(result["secondGeneration"]))
		if first == "" || second == "" || first == second || result["observedEvent"] != true {
			_ = command.Process.Kill()
			<-waitCh
			t.Fatalf("browser spike result is incomplete: %v", result)
		}
		_ = command.Process.Kill()
		<-waitCh
	case runErr := <-waitCh:
		t.Fatalf("Chrome exited before browser proof: %v\n%s", runErr, output.String())
	case <-time.After(60 * time.Second):
		_ = command.Process.Kill()
		<-waitCh
		stageMu.Lock()
		observedStages := append([]string(nil), stages...)
		stageMu.Unlock()
		t.Fatalf("browser spike timed out at stages %v\nchrome: %s", observedStages, output.String())
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func chromeExecutable(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		os.Getenv("MUXVIA_CHROME"),
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"google-chrome", "chromium", "chromium-browser",
	} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if strings.Contains(candidate, string(filepath.Separator)) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved
		}
	}
	t.Skip("Chrome is unavailable for the real browser WASM/WebRTC E2E")
	return ""
}

func runCommand(t *testing.T, workdir, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = workdir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, output)
	}
}

func decodeProto(response http.ResponseWriter, request *http.Request, message proto.Message) bool {
	defer request.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(request.Body, 8<<20))
	if err != nil || proto.Unmarshal(payload, message) != nil {
		http.Error(response, "invalid protobuf request", http.StatusBadRequest)
		return false
	}
	return true
}

func writeProto(response http.ResponseWriter, message proto.Message) {
	payload, err := proto.Marshal(message)
	if err != nil {
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Header().Set("content-type", "application/x-protobuf")
	_, _ = response.Write(payload)
}

func mustJSON(value string) string {
	payload, _ := json.Marshal(value)
	return string(payload)
}
