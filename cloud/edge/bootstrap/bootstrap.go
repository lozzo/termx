// Package bootstrap 实现 Edge 首次安装的本机身份生成与一次性 CSR 注册。
// 私钥只写入 state directory，成功后从配置中原子删除 bootstrap credential。
package bootstrap

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/configsignature"
	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

// FileConfig 是 `/etc/anytty-cloud-edge/config.yaml` 的本机 bootstrap 配置。
// 区域、容量等运营 desired state 不在这里编辑。
type FileConfig struct {
	ControllerOrigin     string `yaml:"controller_origin"`
	RegisterURL          string `yaml:"register_url"`
	EdgeID               string `yaml:"edge_id"`
	BootstrapToken       string `yaml:"bootstrap_token,omitempty"`
	StateDirectory       string `yaml:"state_directory"`
	PublicEndpoint       string `yaml:"public_endpoint"`
	ListenOverride       string `yaml:"listen_override"`
	TURNListenOverride   string `yaml:"turn_listen_override,omitempty"`
	LogLevel             string `yaml:"log_level,omitempty"`
	ControllerAddress    string `yaml:"controller_address,omitempty"`
	ControllerServerName string `yaml:"controller_server_name,omitempty"`
	ConfigKeyID          string `yaml:"config_key_id,omitempty"`
}

// Resolved 是 bootstrap 完成后提供给 Edge runtime composition 的本机文件路径。
type Resolved struct {
	ListenAddress               string
	ControllerAddress           string
	ControllerServerName        string
	EdgeID                      string
	IdentityCertificateFile     string
	IdentityPrivateKeyFile      string
	PublicCertificateFile       string
	PublicPrivateKeyFile        string
	ControllerCAFile            string
	ConfigSigningKeyID          string
	ConfigSigningPublicKeyFile  string
	DesiredConfigCacheFile      string
	ManagedCertificateStateFile string
	TURNListenAddress           string
	TURNPublicEndpoint          string
	TURNRealm                   string
	UsageOutboxFile             string
}

// Resolve 读取配置；未注册时生成或复用本机私钥并消费一次性 bootstrap token。
func Resolve(ctx context.Context, configFile string, client *http.Client) (Resolved, error) {
	configFile = strings.TrimSpace(configFile)
	payload, err := os.ReadFile(configFile)
	if err != nil {
		return Resolved{}, fmt.Errorf("read Edge config: %w", err)
	}
	config := FileConfig{}
	if err := yaml.Unmarshal(payload, &config); err != nil {
		return Resolved{}, fmt.Errorf("decode Edge config: %w", err)
	}
	normalize(&config)
	if config.EdgeID == "" || config.StateDirectory == "" || config.PublicEndpoint == "" || config.ListenOverride == "" {
		return Resolved{}, errors.New("Edge config requires edge_id, state_directory, public_endpoint, and listen_override")
	}
	if err := os.MkdirAll(config.StateDirectory, 0o700); err != nil {
		return Resolved{}, err
	}
	paths := resolvedPaths(config)
	if config.BootstrapToken != "" {
		if client == nil {
			client = &http.Client{Timeout: 30 * time.Second}
		}
		if err := register(ctx, configFile, &config, paths, client); err != nil {
			return Resolved{}, err
		}
	}
	if config.ControllerAddress == "" || config.ControllerServerName == "" || config.ConfigKeyID == "" {
		return Resolved{}, errors.New("Edge registration is incomplete")
	}
	for _, path := range []string{paths.IdentityCertificateFile, paths.IdentityPrivateKeyFile, paths.PublicCertificateFile, paths.PublicPrivateKeyFile, paths.ControllerCAFile, paths.ConfigSigningPublicKeyFile, paths.DesiredConfigCacheFile} {
		if _, err := os.Stat(path); err != nil {
			return Resolved{}, fmt.Errorf("required Edge state %s: %w", filepath.Base(path), err)
		}
	}
	return resolvedPaths(config), nil
}

func register(ctx context.Context, configFile string, config *FileConfig, paths Resolved, client *http.Client) error {
	registerURL, err := url.Parse(config.RegisterURL)
	if err != nil || registerURL.Scheme != "https" || registerURL.Host == "" {
		return errors.New("register_url must be an absolute HTTPS URL")
	}
	identityKey, err := loadOrCreateKey(paths.IdentityPrivateKeyFile)
	if err != nil {
		return err
	}
	publicKey, err := loadOrCreateKey(paths.PublicPrivateKeyFile)
	if err != nil {
		return err
	}
	identityURI, err := securetransport.EdgeIdentityURI(config.EdgeID)
	if err != nil {
		return err
	}
	identityCSR, err := createCSR(identityKey, pkix.Name{CommonName: config.EdgeID}, nil, []*url.URL{identityURI})
	if err != nil {
		return err
	}
	host := config.PublicEndpoint
	if parsedHost, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		host = parsedHost
	}
	if host == "" || !strings.Contains(host, ".") {
		return errors.New("bootstrap public_endpoint is invalid")
	}
	publicCSR, err := createCSR(publicKey, pkix.Name{CommonName: host}, []string{host}, nil)
	if err != nil {
		return err
	}
	request := &cloudv1.RegisterEdgeRequest{EdgeId: config.EdgeID, BootstrapToken: config.BootstrapToken, IdentityCsrPem: identityCSR, PublicCsrPem: publicCSR}
	body, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(request)
	if err != nil {
		return err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, config.RegisterURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("register Edge: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("register Edge returned HTTP %d", response.StatusCode)
	}
	registration := &cloudv1.RegisterEdgeResponse{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(responseBody, registration); err != nil {
		return fmt.Errorf("decode Edge registration: %w", err)
	}
	if registration.GetEdgeId() != config.EdgeID || len(registration.GetConfigSigningPublicKey()) != ed25519.PublicKeySize {
		return errors.New("Edge registration response identity or config key is invalid")
	}
	desired, err := configsignature.Verify(registration.GetDesiredConfig(), registration.GetConfigKeyId(), ed25519.PublicKey(registration.GetConfigSigningPublicKey()))
	if err != nil || desired.GetEdgeId() != config.EdgeID || !strings.EqualFold(desired.GetPublicEndpoint(), config.PublicEndpoint) {
		return errors.New("Edge registration desired config is invalid")
	}
	stateFiles := []struct {
		path    string
		payload []byte
	}{
		{paths.IdentityCertificateFile, registration.GetIdentityCertificatePem()},
		{paths.PublicCertificateFile, registration.GetPublicCertificatePem()},
		{filepath.Join(config.StateDirectory, "edge-ca.pem"), registration.GetEdgeCaCertificatePem()},
		{paths.ControllerCAFile, registration.GetControllerCaCertificatePem()},
		{paths.ConfigSigningPublicKeyFile, registration.GetConfigSigningPublicKey()},
	}
	for _, file := range stateFiles {
		if err := writeState(file.path, file.payload, 0o600); err != nil {
			return err
		}
	}
	signedPayload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(registration.GetDesiredConfig())
	if err != nil {
		return err
	}
	if err := writeState(paths.DesiredConfigCacheFile, signedPayload, 0o600); err != nil {
		return err
	}
	config.BootstrapToken = ""
	config.ControllerAddress = registration.GetControllerAddress()
	config.ControllerServerName = registration.GetControllerServerName()
	config.ConfigKeyID = registration.GetConfigKeyId()
	updated, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return atomicWrite(configFile, updated, 0o600)
}

func resolvedPaths(config FileConfig) Resolved {
	publicHost := config.PublicEndpoint
	if host, _, err := net.SplitHostPort(config.PublicEndpoint); err == nil {
		publicHost = strings.Trim(host, "[]")
	}
	return Resolved{
		ListenAddress: config.ListenOverride, ControllerAddress: config.ControllerAddress, ControllerServerName: config.ControllerServerName, EdgeID: config.EdgeID,
		IdentityCertificateFile: filepath.Join(config.StateDirectory, "identity-cert.pem"), IdentityPrivateKeyFile: filepath.Join(config.StateDirectory, "identity-key.pem"),
		PublicCertificateFile: filepath.Join(config.StateDirectory, "public-cert.pem"), PublicPrivateKeyFile: filepath.Join(config.StateDirectory, "public-key.pem"),
		ControllerCAFile: filepath.Join(config.StateDirectory, "controller-ca.pem"), ConfigSigningKeyID: config.ConfigKeyID,
		ConfigSigningPublicKeyFile: filepath.Join(config.StateDirectory, "config-signing-public.key"), DesiredConfigCacheFile: filepath.Join(config.StateDirectory, "desired-config.pb"),
		ManagedCertificateStateFile: filepath.Join(config.StateDirectory, "managed-certificate.pb"),
		TURNListenAddress:           config.TURNListenOverride, TURNPublicEndpoint: net.JoinHostPort(publicHost, "3478"), TURNRealm: publicHost, UsageOutboxFile: filepath.Join(config.StateDirectory, "usage-outbox.db"),
	}
}

func normalize(config *FileConfig) {
	config.ControllerOrigin = strings.TrimRight(strings.TrimSpace(config.ControllerOrigin), "/")
	config.RegisterURL = strings.TrimSpace(config.RegisterURL)
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.BootstrapToken = strings.TrimSpace(config.BootstrapToken)
	config.StateDirectory = strings.TrimSpace(config.StateDirectory)
	config.PublicEndpoint = strings.TrimSpace(config.PublicEndpoint)
	config.ListenOverride = strings.TrimSpace(config.ListenOverride)
	config.TURNListenOverride = strings.TrimSpace(config.TURNListenOverride)
	if config.TURNListenOverride == "" {
		config.TURNListenOverride = "0.0.0.0:3478"
	}
	config.ControllerAddress = strings.TrimSpace(config.ControllerAddress)
	config.ControllerServerName = strings.TrimSpace(config.ControllerServerName)
	config.ConfigKeyID = strings.TrimSpace(config.ConfigKeyID)
}

func loadOrCreateKey(path string) (*ecdsa.PrivateKey, error) {
	if payload, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(payload)
		if block == nil {
			return nil, errors.New("Edge private key PEM is invalid")
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		if typed, ok := key.(*ecdsa.PrivateKey); ok {
			return typed, nil
		}
		return nil, errors.New("Edge private key is not ECDSA")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writeState(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func createCSR(key *ecdsa.PrivateKey, subject pkix.Name, dnsNames []string, uris []*url.URL) ([]byte, error) {
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: subject, DNSNames: dnsNames, URIs: uris}, key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

func writeState(path string, payload []byte, mode os.FileMode) error {
	if len(payload) == 0 {
		return fmt.Errorf("refuse to write empty Edge state %s", filepath.Base(path))
	}
	return atomicWrite(path, payload, mode)
}

func atomicWrite(path string, payload []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, payload, mode); err != nil {
		return err
	}
	if err := os.Chmod(temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
