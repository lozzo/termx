// Package securetransport 提供 Cloud 跨进程连接共用的 TLS 1.3 配置与 Edge 证书身份解析。
package securetransport

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	edgeIdentityScheme = "spiffe"
	edgeIdentityHost   = "anytty.com"
	edgeIdentityPrefix = "/edge/"
)

// ServerOptions 定义 Cloud gRPC/HTTPS server 的证书材料。
// ClientCAFile 非空时 server 强制校验 mTLS 客户端证书。
type ServerOptions struct {
	CertificateFile string
	PrivateKeyFile  string
	ClientCAFile    string
}

// NewServerTLSConfig 加载 server 证书并返回最低 TLS 1.3 的不可变装配。
// 文件不存在、PEM 无效或 Client CA 为空时显式失败。
func NewServerTLSConfig(options ServerOptions) (*tls.Config, error) {
	config, _, err := NewReloadableServerTLSConfig(options)
	return config, err
}

// ReloadableCertificate 是 server TLS 当前证书的原子内存 owner。
// Replace 只接受调用方已经完整校验过的不可变 key pair。
type ReloadableCertificate struct {
	current atomic.Pointer[tls.Certificate]
}

// NewReloadableServerTLSConfig 加载初始文件，并把后续新握手绑定到可原子替换的 loader。
func NewReloadableServerTLSConfig(options ServerOptions) (*tls.Config, *ReloadableCertificate, error) {
	certificate, err := tls.LoadX509KeyPair(strings.TrimSpace(options.CertificateFile), strings.TrimSpace(options.PrivateKeyFile))
	if err != nil {
		return nil, nil, fmt.Errorf("load server certificate: %w", err)
	}
	loader := &ReloadableCertificate{}
	loader.Replace(&certificate)
	config := &tls.Config{
		MinVersion: tls.VersionTLS13,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			current := loader.current.Load()
			if current == nil {
				return nil, errors.New("server certificate is unavailable")
			}
			return current, nil
		},
		NextProtos: []string{"h2", "http/1.1"},
	}
	if strings.TrimSpace(options.ClientCAFile) == "" {
		return config, loader, nil
	}
	pool, err := loadCertPool(options.ClientCAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load client CA: %w", err)
	}
	config.ClientAuth = tls.RequireAndVerifyClientCert
	config.ClientCAs = pool
	return config, loader, nil
}

// Replace 原子替换后续 TLS 握手使用的证书；调用后不再修改输入对象。
func (loader *ReloadableCertificate) Replace(certificate *tls.Certificate) {
	if loader == nil || certificate == nil {
		return
	}
	loader.current.Store(certificate)
}

// ValidatedServerPair 是一次完整校验后的 server key pair 和叶证书。
type ValidatedServerPair struct {
	Certificate *tls.Certificate
	Leaf        *x509.Certificate
}

// ValidateServerPair 校验证书链、私钥、DNS SAN、可选公网入口和有效期。
// now 为零值时只用于恢复本机旧证书，不检查有效期，以便 Edge 仍能连回 Controller 收敛。
func ValidateServerPair(certificatePEM, privateKeyPEM []byte, publicEndpoint string, now time.Time) (*ValidatedServerPair, error) {
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("certificate and private key do not match: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("certificate chain contains no certificate")
	}
	var leaf *x509.Certificate
	for index, der := range pair.Certificate {
		certificate, parseErr := x509.ParseCertificate(der)
		if parseErr != nil {
			return nil, fmt.Errorf("parse certificate chain item %d: %w", index+1, parseErr)
		}
		if index == 0 {
			leaf = certificate
		}
	}
	if len(leaf.DNSNames) == 0 {
		return nil, errors.New("certificate must contain at least one DNS SAN")
	}
	if !now.IsZero() {
		now = now.UTC()
		if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
			return nil, errors.New("certificate is not currently valid")
		}
	}
	if endpoint := strings.TrimSpace(publicEndpoint); endpoint != "" {
		host := endpoint
		if parsed, _, splitErr := net.SplitHostPort(host); splitErr == nil {
			host = strings.Trim(parsed, "[]")
		}
		if err := leaf.VerifyHostname(host); err != nil {
			return nil, fmt.Errorf("certificate does not cover public endpoint %q: %w", host, err)
		}
	}
	pair.Leaf = leaf
	return &ValidatedServerPair{Certificate: &pair, Leaf: leaf}, nil
}

// ClientOptions 定义 Edge 连接 Controller 时的 mTLS 材料和 TLS server name。
type ClientOptions struct {
	CertificateFile string
	PrivateKeyFile  string
	RootCAFile      string
	ServerName      string
}

// NewClientTLSConfig 加载 EdgeIdentity 客户端证书与 Controller CA。
// ServerName 是必填信任边界，禁止 InsecureSkipVerify 或系统 CA fallback。
func NewClientTLSConfig(options ClientOptions) (*tls.Config, error) {
	serverName := strings.TrimSpace(options.ServerName)
	if serverName == "" {
		return nil, errors.New("controller TLS server name is required")
	}
	certificate, err := tls.LoadX509KeyPair(strings.TrimSpace(options.CertificateFile), strings.TrimSpace(options.PrivateKeyFile))
	if err != nil {
		return nil, fmt.Errorf("load client certificate: %w", err)
	}
	pool, err := loadCertPool(options.RootCAFile)
	if err != nil {
		return nil, fmt.Errorf("load controller CA: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		RootCAs:      pool,
		ServerName:   serverName,
		NextProtos:   []string{"h2"},
	}, nil
}

// EdgeIdentityURI 返回 EdgeIdentity 证书唯一允许的 URI SAN。
// Edge ID 是 Controller 生成的不透明 ID，不允许路径分隔符或空白。
func EdgeIdentityURI(edgeID string) (*url.URL, error) {
	edgeID = strings.TrimSpace(edgeID)
	if edgeID == "" || strings.ContainsAny(edgeID, "/\\") || strings.ContainsFunc(edgeID, func(value rune) bool { return value <= ' ' }) {
		return nil, fmt.Errorf("invalid edge ID %q", edgeID)
	}
	return &url.URL{Scheme: edgeIdentityScheme, Host: edgeIdentityHost, Path: edgeIdentityPrefix + edgeID}, nil
}

// EdgeIDFromCertificate 从已完成 CA 校验的客户端证书解析 Edge ID。
// 缺失、重复或格式错误的 Edge URI SAN 都会拒绝连接。
func EdgeIDFromCertificate(certificate *x509.Certificate) (string, error) {
	if certificate == nil {
		return "", errors.New("client certificate is missing")
	}
	var edgeID string
	for _, identity := range certificate.URIs {
		if identity == nil || identity.Scheme != edgeIdentityScheme || identity.Host != edgeIdentityHost || !strings.HasPrefix(identity.Path, edgeIdentityPrefix) {
			continue
		}
		if identity.User != nil || identity.Opaque != "" || identity.RawPath != "" || identity.ForceQuery || identity.RawQuery != "" || identity.Fragment != "" {
			return "", errors.New("Edge URI SAN contains unsupported URI components")
		}
		candidate := strings.TrimPrefix(identity.Path, edgeIdentityPrefix)
		if _, err := EdgeIdentityURI(candidate); err != nil {
			return "", fmt.Errorf("invalid Edge URI SAN: %w", err)
		}
		if edgeID != "" {
			return "", errors.New("client certificate contains multiple Edge URI SANs")
		}
		edgeID = candidate
	}
	if edgeID == "" {
		return "", errors.New("client certificate does not contain an Edge URI SAN")
	}
	return edgeID, nil
}

func loadCertPool(path string) (*x509.CertPool, error) {
	payload, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(payload) {
		return nil, errors.New("PEM contains no certificates")
	}
	return pool, nil
}
