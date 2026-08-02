// Package install 实现 Edge 一次性安装脚本、CSR 注册和固定 artifact 验签材料。
// Controller 只接收公钥 CSR；EdgeIdentity 与公网 TLS 私钥始终由 Edge 本机持有。
package install

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Config 是安装服务的受控部署输入；所有密钥都来自权限受控文件。
type Config struct {
	Edges                 *edgeconfig.Service
	PublicOrigin          string
	ControllerAddress     string
	ControllerServerName  string
	EdgeCACertificateFile string
	EdgeCAPrivateKeyFile  string
	ControllerCAFile      string
	ArtifactFile          string
	ArtifactVersion       string
	ArtifactSigningKey    ed25519.PrivateKey
	// CertificateValidity is retained as a construction fallback for tests and
	// older composition roots. Production config sets the two purposes explicitly.
	CertificateValidity         time.Duration
	IdentityCertificateValidity time.Duration
	PublicCertificateValidity   time.Duration
	AuditIdentityCertificate    func(context.Context, string, string, []byte, time.Time, time.Time) error
	Now                         func() time.Time
}

// Service 持有 CA signer、固定 Edge artifact 和安装模板，不持有 claim 真值。
type Service struct {
	edges                       *edgeconfig.Service
	publicOrigin                string
	controllerAddress           string
	controllerServerName        string
	caCertificate               *x509.Certificate
	caCertificatePEM            []byte
	caKey                       crypto.Signer
	controllerCAPEM             []byte
	artifact                    []byte
	artifactVersion             string
	artifactDigest              string
	artifactSignature           []byte
	artifactPublicPEM           []byte
	identityCertificateValidity time.Duration
	publicCertificateValidity   time.Duration
	certificateValidity         time.Duration
	auditIdentityCertificate    func(context.Context, string, string, []byte, time.Time, time.Time) error
	now                         func() time.Time
}

// NewService 加载并验证 CA、artifact 与发布签名，失败时不提供部分安装服务。
func NewService(config Config) (*Service, error) {
	config.PublicOrigin = strings.TrimRight(strings.TrimSpace(config.PublicOrigin), "/")
	config.ControllerAddress = strings.TrimSpace(config.ControllerAddress)
	config.ControllerServerName = strings.TrimSpace(config.ControllerServerName)
	config.ArtifactVersion = strings.TrimSpace(config.ArtifactVersion)
	if config.IdentityCertificateValidity <= 0 {
		config.IdentityCertificateValidity = config.CertificateValidity
	}
	if config.PublicCertificateValidity <= 0 {
		config.PublicCertificateValidity = config.CertificateValidity
	}
	if config.Edges == nil || config.PublicOrigin == "" || config.ControllerAddress == "" || config.ControllerServerName == "" || config.ArtifactVersion == "" || len(config.ArtifactSigningKey) != ed25519.PrivateKeySize || config.IdentityCertificateValidity <= 0 || config.PublicCertificateValidity <= 0 {
		return nil, errors.New("Edge service, public/controller origins, artifact version/signing key, and certificate validity are required")
	}
	caPEM, caCertificate, caKey, err := loadCA(config.EdgeCACertificateFile, config.EdgeCAPrivateKeyFile)
	if err != nil {
		return nil, err
	}
	controllerCAPEM, err := os.ReadFile(config.ControllerCAFile)
	if err != nil {
		return nil, fmt.Errorf("read Controller CA: %w", err)
	}
	artifact, err := os.ReadFile(config.ArtifactFile)
	if err != nil {
		return nil, fmt.Errorf("read Edge artifact: %w", err)
	}
	digest := sha256.Sum256(artifact)
	publicDER, err := x509.MarshalPKIXPublicKey(config.ArtifactSigningKey.Public())
	if err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		edges: config.Edges, publicOrigin: config.PublicOrigin, controllerAddress: config.ControllerAddress, controllerServerName: config.ControllerServerName,
		caCertificate: caCertificate, caCertificatePEM: caPEM, caKey: caKey, controllerCAPEM: controllerCAPEM,
		artifact: artifact, artifactVersion: config.ArtifactVersion, artifactDigest: hex.EncodeToString(digest[:]),
		artifactSignature: ed25519.Sign(config.ArtifactSigningKey, artifact), artifactPublicPEM: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}),
		identityCertificateValidity: config.IdentityCertificateValidity, publicCertificateValidity: config.PublicCertificateValidity,
		certificateValidity:      config.CertificateValidity,
		auditIdentityCertificate: config.AuditIdentityCertificate, now: config.Now,
	}, nil
}

// Artifact 返回固定 Linux/amd64 Edge artifact；调用方不得修改返回内容。
func (service *Service) Artifact() []byte { return append([]byte(nil), service.artifact...) }

// InstallScript 原子消费 install claim，并返回只对目标 Edge 有效的 bootstrap 脚本。
func (service *Service) InstallScript(ctx context.Context, token string) (string, error) {
	edge, bootstrap, _, err := service.edges.ConsumeInstallClaim(ctx, token)
	if err != nil {
		return "", err
	}
	endpointPort := "443"
	if _, port, splitErr := net.SplitHostPort(edge.PublicEndpoint); splitErr == nil {
		endpointPort = port
	}
	artifactURL := service.publicOrigin + "/artifacts/anytty-cloud-edge-linux-amd64"
	signature := base64.StdEncoding.EncodeToString(service.artifactSignature)
	return fmt.Sprintf(`#!/bin/sh
set -eu

[ "$(id -u)" -eq 0 ] || { echo "must run as root" >&2; exit 1; }
[ "$(uname -s)" = "Linux" ] || { echo "Linux is required" >&2; exit 1; }
[ "$(uname -m)" = "x86_64" ] || { echo "this release supports linux/amd64 only" >&2; exit 1; }
for command in curl openssl sha256sum systemctl useradd mktemp; do command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 1; }; done

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM
curl -fsSL %s -o "$tmp_dir/anytty-cloud-edge"
printf '%%s  %%s\n' %s "$tmp_dir/anytty-cloud-edge" | sha256sum -c -
printf '%%s' %s | base64 -d > "$tmp_dir/artifact.sig"
cat > "$tmp_dir/artifact-public.pem" <<'ANYTTY_RELEASE_KEY'
%sANYTTY_RELEASE_KEY
openssl pkeyutl -verify -pubin -inkey "$tmp_dir/artifact-public.pem" -rawin -in "$tmp_dir/anytty-cloud-edge" -sigfile "$tmp_dir/artifact.sig" >/dev/null

id anytty-edge >/dev/null 2>&1 || useradd --system --home /var/lib/anytty-cloud-edge --shell /usr/sbin/nologin anytty-edge
install -d -m 0755 /opt/anytty-cloud-edge/releases/%s
install -m 0755 "$tmp_dir/anytty-cloud-edge" /opt/anytty-cloud-edge/releases/%s/anytty-cloud-edge
ln -sfn /opt/anytty-cloud-edge/releases/%s /opt/anytty-cloud-edge/current
install -d -o anytty-edge -g anytty-edge -m 0700 /var/lib/anytty-cloud-edge
# Edge 必须在首次注册成功后原子替换配置文件，目录只向专用服务组开放写权限。
install -d -o root -g anytty-edge -m 0770 /etc/anytty-cloud-edge
cat > /etc/anytty-cloud-edge/config.yaml <<'ANYTTY_EDGE_CONFIG'
controller_origin: %s
register_url: %s
edge_id: %s
bootstrap_token: %s
state_directory: /var/lib/anytty-cloud-edge
public_endpoint: %s
listen_override: 0.0.0.0:%s
turn_listen_override: 0.0.0.0:3478
log_level: info
ANYTTY_EDGE_CONFIG
chown anytty-edge:anytty-edge /etc/anytty-cloud-edge/config.yaml
chmod 0600 /etc/anytty-cloud-edge/config.yaml
cat > /etc/systemd/system/anytty-cloud-edge.service <<'ANYTTY_EDGE_SYSTEMD'
[Unit]
Description=AnyTTY Cloud Edge
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=anytty-edge
Group=anytty-edge
StateDirectory=anytty-cloud-edge
StateDirectoryMode=0700
ExecStart=/opt/anytty-cloud-edge/current/anytty-cloud-edge --config=/etc/anytty-cloud-edge/config.yaml
Restart=on-failure
RestartSec=3
TimeoutStopSec=20
LimitNOFILE=1048576
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/anytty-cloud-edge /etc/anytty-cloud-edge
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
RestrictSUIDSGID=true
RestrictRealtime=true
LockPersonality=true
SystemCallArchitectures=native
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=multi-user.target
ANYTTY_EDGE_SYSTEMD
systemctl daemon-reload
systemctl enable anytty-cloud-edge.service
systemctl restart anytty-cloud-edge.service
echo "AnyTTY Cloud Edge installed: %s"
`, strconv.Quote(artifactURL), strconv.Quote(service.artifactDigest), strconv.Quote(signature), string(service.artifactPublicPEM), service.artifactVersion, service.artifactVersion, service.artifactVersion, strconv.Quote(service.publicOrigin), strconv.Quote(service.publicOrigin+"/api/install/register"), strconv.Quote(edge.ID), strconv.Quote(bootstrap), strconv.Quote(edge.PublicEndpoint), endpointPort, edge.ID), nil
}

// Register 校验两个 CSR 和目标 endpoint、原子消费 bootstrap，再签发专属 identity/public 证书。
func (service *Service) Register(ctx context.Context, request *cloudv1.RegisterEdgeRequest) (*cloudv1.RegisterEdgeResponse, error) {
	if request == nil || strings.TrimSpace(request.GetEdgeId()) == "" || strings.TrimSpace(request.GetBootstrapToken()) == "" {
		return nil, errors.New("Edge ID and bootstrap token are required")
	}
	identityCSR, err := parseCSR(request.GetIdentityCsrPem())
	if err != nil {
		return nil, fmt.Errorf("identity CSR: %w", err)
	}
	publicCSR, err := parseCSR(request.GetPublicCsrPem())
	if err != nil {
		return nil, fmt.Errorf("public CSR: %w", err)
	}
	expectedURI, err := securetransport.EdgeIdentityURI(request.GetEdgeId())
	if err != nil || validateIdentityCSR(identityCSR, expectedURI) != nil {
		return nil, errors.New("identity CSR does not contain the expected Edge URI SAN")
	}
	edge, err := service.edges.GetEdge(ctx, request.GetEdgeId())
	if err != nil {
		return nil, err
	}
	if err := validatePublicCSR(publicCSR, edge.PublicEndpoint); err != nil {
		return nil, err
	}
	csrDigest := sha256.Sum256(identityCSR.Raw)
	edge, err = service.edges.ConsumeBootstrapClaim(ctx, request.GetBootstrapToken(), request.GetEdgeId(), csrDigest[:])
	if err != nil {
		return nil, err
	}
	identityCertificate, err := service.issue(identityCSR, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, service.identityValidity())
	if err != nil {
		return nil, err
	}
	publicCertificate, err := service.issue(publicCSR, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, service.publicValidity())
	if err != nil {
		return nil, err
	}
	publicCertificate = append(publicCertificate, service.caCertificatePEM...)
	keyID, configPublicKey := service.edges.SigningPublicKey()
	return &cloudv1.RegisterEdgeResponse{
		EdgeId: edge.ID, IdentityCertificatePem: identityCertificate, PublicCertificatePem: publicCertificate,
		EdgeCaCertificatePem: append([]byte(nil), service.caCertificatePEM...), ControllerCaCertificatePem: append([]byte(nil), service.controllerCAPEM...),
		ControllerAddress: service.controllerAddress, ControllerServerName: service.controllerServerName,
		DesiredConfig: edge.SignedConfig, ConfigKeyId: keyID, ConfigSigningPublicKey: configPublicKey,
	}, nil
}

// RenewIdentityCertificate signs a fresh EdgeIdentity CSR only for the Edge
// already authenticated by EdgeControl. The caller owns matching the request's
// current fingerprint to the actual TLS peer certificate.
func (service *Service) RenewIdentityCertificate(ctx context.Context, edgeID string, request *cloudv1.EdgeIdentityRenewRequest) (*cloudv1.EdgeIdentityRenewResponse, error) {
	edgeID = strings.TrimSpace(edgeID)
	if request == nil || edgeID == "" || strings.TrimSpace(request.GetRequestId()) == "" || request.GetRequestedAt() == nil || request.GetRequestedAt().CheckValid() != nil || len(request.GetCurrentCertificateSha256()) != sha256.Size {
		return nil, errors.New("EdgeIdentity renewal request is incomplete")
	}
	edge, err := service.edges.GetEdge(ctx, edgeID)
	if err != nil {
		return nil, err
	}
	if !edge.Enabled {
		return nil, errors.New("disabled Edge cannot renew its identity certificate")
	}
	csr, err := parseCSR(request.GetCsrPem())
	if err != nil {
		return nil, fmt.Errorf("identity renewal CSR: %w", err)
	}
	expectedURI, err := securetransport.EdgeIdentityURI(edgeID)
	if err != nil {
		return nil, err
	}
	if err := validateIdentityCSR(csr, expectedURI); err != nil {
		return nil, err
	}
	certificatePEM, err := service.issue(csr, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, service.identityValidity())
	if err != nil {
		return nil, err
	}
	block, trailing := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(trailing)) != 0 {
		return nil, errors.New("issued EdgeIdentity certificate is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	now := service.now().UTC()
	if service.auditIdentityCertificate != nil {
		if err := service.auditIdentityCertificate(ctx, edgeID, "issued", fingerprint[:], certificate.NotAfter.UTC(), now); err != nil {
			return nil, fmt.Errorf("audit EdgeIdentity certificate issuance: %w", err)
		}
	}
	return &cloudv1.EdgeIdentityRenewResponse{
		RequestId: request.GetRequestId(), CertificatePem: certificatePEM, CertificateSha256: fingerprint[:], NotAfter: timestamppb.New(certificate.NotAfter.UTC()),
	}, nil
}

func (service *Service) RecordIdentityCertificateApplied(ctx context.Context, edgeID string, applied *cloudv1.EdgeIdentityApplied) error {
	if applied == nil || strings.TrimSpace(edgeID) == "" || strings.TrimSpace(applied.GetRequestId()) == "" || len(applied.GetCertificateSha256()) != sha256.Size || applied.GetNotAfter() == nil || applied.GetNotAfter().CheckValid() != nil {
		return errors.New("EdgeIdentity applied receipt is invalid")
	}
	if service.auditIdentityCertificate == nil {
		return nil
	}
	stage := "applied"
	if !applied.GetApplied() {
		stage = "apply_failed"
	}
	return service.auditIdentityCertificate(ctx, strings.TrimSpace(edgeID), stage, applied.GetCertificateSha256(), applied.GetNotAfter().AsTime().UTC(), service.now().UTC())
}

func (service *Service) RecoverIdentityCertificate(ctx context.Context, request *cloudv1.RecoverEdgeIdentityRequest) (*cloudv1.RecoverEdgeIdentityResponse, error) {
	if request == nil || strings.TrimSpace(request.GetEdgeId()) == "" || strings.TrimSpace(request.GetRecoveryToken()) == "" {
		return nil, errors.New("Edge identity recovery request is incomplete")
	}
	csr, err := parseCSR(request.GetIdentityCsrPem())
	if err != nil {
		return nil, fmt.Errorf("identity recovery CSR: %w", err)
	}
	expectedURI, err := securetransport.EdgeIdentityURI(request.GetEdgeId())
	if err != nil {
		return nil, err
	}
	if err := validateIdentityCSR(csr, expectedURI); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(csr.Raw)
	if _, err := service.edges.ConsumeIdentityRecoveryClaim(ctx, request.GetRecoveryToken(), request.GetEdgeId(), digest[:]); err != nil {
		return nil, err
	}
	certificatePEM, err := service.issue(csr, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, service.identityValidity())
	if err != nil {
		return nil, err
	}
	block, trailing := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(trailing)) != 0 {
		return nil, errors.New("issued recovered EdgeIdentity certificate is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	now := service.now().UTC()
	if service.auditIdentityCertificate != nil {
		if err := service.auditIdentityCertificate(ctx, request.GetEdgeId(), "recovery_issued", fingerprint[:], certificate.NotAfter.UTC(), now); err != nil {
			return nil, fmt.Errorf("audit recovered EdgeIdentity certificate issuance: %w", err)
		}
	}
	return &cloudv1.RecoverEdgeIdentityResponse{IdentityCertificatePem: certificatePEM, CertificateSha256: fingerprint[:], NotAfter: timestamppb.New(certificate.NotAfter.UTC())}, nil
}

func validatePublicCSR(csr *x509.CertificateRequest, publicEndpoint string) error {
	host := strings.TrimSpace(publicEndpoint)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = strings.Trim(parsedHost, "[]")
	}
	if ip := net.ParseIP(host); ip != nil {
		if len(csr.IPAddresses) != 1 || !csr.IPAddresses[0].Equal(ip) || len(csr.DNSNames) != 0 {
			return errors.New("public CSR IP SAN does not match Edge endpoint")
		}
		return nil
	}
	if len(csr.DNSNames) != 1 || !strings.EqualFold(csr.DNSNames[0], host) || len(csr.IPAddresses) != 0 {
		return errors.New("public CSR DNS SAN does not match Edge endpoint")
	}
	return nil
}

func (service *Service) issue(csr *x509.CertificateRequest, usages []x509.ExtKeyUsage, validity time.Duration) ([]byte, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	now := service.now()
	template := &x509.Certificate{SerialNumber: serial, Subject: csr.Subject, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(validity), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: usages, DNSNames: csr.DNSNames, IPAddresses: csr.IPAddresses, URIs: csr.URIs}
	der, err := x509.CreateCertificate(rand.Reader, template, service.caCertificate, csr.PublicKey, service.caKey)
	if err != nil {
		return nil, fmt.Errorf("sign Edge certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func (service *Service) identityValidity() time.Duration {
	if service.identityCertificateValidity > 0 {
		return service.identityCertificateValidity
	}
	return service.certificateValidity
}

func (service *Service) publicValidity() time.Duration {
	if service.publicCertificateValidity > 0 {
		return service.publicCertificateValidity
	}
	return service.certificateValidity
}

func validateIdentityCSR(csr *x509.CertificateRequest, expectedURI *url.URL) error {
	if csr == nil || expectedURI == nil || len(csr.URIs) != 1 || csr.URIs[0] == nil || csr.URIs[0].String() != expectedURI.String() || len(csr.DNSNames) != 0 || len(csr.IPAddresses) != 0 || len(csr.EmailAddresses) != 0 {
		return errors.New("identity CSR does not contain the exact expected Edge URI SAN")
	}
	key, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return errors.New("EdgeIdentity CSR must use an ECDSA P-256 key")
	}
	return nil
}

func loadCA(certificateFile, privateKeyFile string) ([]byte, *x509.Certificate, crypto.Signer, error) {
	certificatePEM, err := os.ReadFile(certificateFile)
	if err != nil {
		return nil, nil, nil, err
	}
	certificateBlock, _ := pem.Decode(certificatePEM)
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" {
		return nil, nil, nil, errors.New("Edge CA certificate PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil || !certificate.IsCA {
		return nil, nil, nil, errors.New("Edge CA certificate is not a CA")
	}
	keyPEM, err := os.ReadFile(privateKeyFile)
	if err != nil {
		return nil, nil, nil, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, nil, errors.New("Edge CA private key PEM is invalid")
	}
	var key any
	if keyBlock.Type == "EC PRIVATE KEY" {
		key, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	} else {
		key, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, nil, nil, errors.New("Edge CA private key cannot sign")
	}
	certificatePublic, certificateErr := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	signerPublic, signerErr := x509.MarshalPKIXPublicKey(signer.Public())
	if certificateErr != nil || signerErr != nil || !bytes.Equal(certificatePublic, signerPublic) {
		return nil, nil, nil, errors.New("Edge CA certificate and private key do not match")
	}
	return certificatePEM, certificate, signer, nil
}

func parseCSR(payload []byte) (*x509.CertificateRequest, error) {
	block, trailing := pem.Decode(payload)
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(bytes.TrimSpace(trailing)) != 0 {
		return nil, errors.New("PEM certificate request is required")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, err
	}
	if csr.Subject.String() == (pkix.Name{}).String() {
		return nil, errors.New("CSR subject is required")
	}
	return csr, nil
}
