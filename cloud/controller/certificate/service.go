// Package certificate 拥有 Controller 侧证书档案、Edge 绑定和 desired/applied revision。
// PostgreSQL 只保存元数据，证书与私钥由 SecretStore 单独保存。
package certificate

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrNotFound 表示证书档案或 Edge 绑定不存在。
	ErrNotFound = errors.New("certificate profile or binding was not found")
	// ErrRevisionConflict 表示运营 mutation 基于过期 revision。
	ErrRevisionConflict = errors.New("certificate revision conflict")
)

const maxPEMSize = 1 << 20

// Profile 是证书档案的持久元数据；SecretRef 只在 Controller 内部使用。
type Profile struct {
	ID          string
	Name        string
	DNSNames    []string
	Fingerprint string
	NotBefore   time.Time
	NotAfter    time.Time
	Revision    uint64
	SecretRef   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Bindings    []Binding
}

// Binding 是一个 Edge 的持久证书选择和最近应用结果。
type Binding struct {
	EdgeID           string
	EdgeName         string
	PublicEndpoint   string
	ProfileID        string
	ProfileName      string
	BindingRevision  uint64
	DesiredRevision  uint64
	AppliedProfileID string
	AppliedRevision  uint64
	LastErrorCode    string
	LastErrorMessage string
	AppliedAt        time.Time
	UpdatedAt        time.Time
}

// Store 是证书元数据、绑定和审计的 PostgreSQL 事务边界。
type Store interface {
	ListCertificateProfiles(context.Context) ([]Profile, error)
	GetCertificateProfile(context.Context, string) (Profile, error)
	CreateCertificateProfile(context.Context, Profile, string) error
	ReplaceCertificateProfile(context.Context, uint64, Profile, string) ([]Binding, error)
	GetCertificateBinding(context.Context, string) (Binding, bool, error)
	BindCertificateProfile(context.Context, edgeconfig.Edge, Profile, uint64, string, time.Time) (Binding, error)
	UnbindCertificateProfile(context.Context, string, uint64, string, time.Time) (Binding, error)
	RecordCertificateApplied(context.Context, string, *cloudv1.CertificateApplied, time.Time) error
}

// SecretStore 保存当前证书链和私钥；引用必须不可猜测且不能进入运营 API。
type SecretStore interface {
	Put([]byte, []byte) (string, error)
	Read(string) ([]byte, []byte, error)
	Reconcile([]string) error
}

// Dispatcher 通知指定在线 Edge 重新读取当前持久 desired；通知不携带事务外旧证书包。
type Dispatcher interface {
	RefreshCertificate(context.Context, string) error
}

// DispatcherFunc 把 composition root 的闭包适配为证书下发边界。
type DispatcherFunc func(context.Context, string) error

// RefreshCertificate 调用闭包通知 Edge writer 读取最新 desired。
func (function DispatcherFunc) RefreshCertificate(ctx context.Context, edgeID string) error {
	return function(ctx, edgeID)
}

// Config 固定证书服务的持久、secret、Edge 配置和在线投影 owner。
type Config struct {
	Store      Store
	Secrets    SecretStore
	Edges      *edgeconfig.Service
	Dispatcher Dispatcher
	Online     func(context.Context, string) (bool, error)
	Now        func() time.Time
	Logger     *slog.Logger
}

// Service 编排上传、绑定和自动下发，不缓存在线状态或私钥。
type Service struct {
	config      Config
	secretState sync.RWMutex
}

// New 创建证书服务；缺失任一真值 owner 时 fail closed。
func New(config Config) (*Service, error) {
	if config.Store == nil || config.Secrets == nil || config.Edges == nil || config.Dispatcher == nil || config.Online == nil {
		return nil, errors.New("certificate store, secret store, Edge store, dispatcher, and online projection are required")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return &Service{config: config}, nil
}

// ListProfiles 返回不含 PEM 和 secret 引用的运营投影。
func (service *Service) ListProfiles(ctx context.Context) (*cloudv1.ListCertificateProfilesResponse, error) {
	profiles, err := service.config.Store.ListCertificateProfiles(ctx)
	if err != nil {
		return nil, err
	}
	response := &cloudv1.ListCertificateProfilesResponse{Profiles: make([]*cloudv1.CertificateProfile, 0, len(profiles))}
	for _, profile := range profiles {
		response.Profiles = append(response.Profiles, service.projectProfile(ctx, profile))
	}
	return response, nil
}

// UploadProfile 创建或替换档案当前内容，并尽力立即通知全部在线绑定 Edge。
// 下发失败只保留 pending 状态；不能回滚已经提交的运营配置。
func (service *Service) UploadProfile(ctx context.Context, request *cloudv1.UploadCertificateProfileRequest, actorID string) (*cloudv1.UploadCertificateProfileResponse, error) {
	if request == nil || strings.TrimSpace(request.GetName()) == "" || strings.TrimSpace(actorID) == "" {
		return nil, errors.New("certificate profile name and operator identity are required")
	}
	profileID := strings.TrimSpace(request.GetCertificateProfileId())
	if profileID == "" && request.GetExpectedRevision() != 0 {
		return nil, errors.New("new certificate profile expected revision must be zero")
	}
	if profileID != "" && request.GetExpectedRevision() == 0 {
		return nil, errors.New("existing certificate profile expected revision is required")
	}
	metadata, err := ValidatePair(request.GetCertificateChainPem(), request.GetPrivateKeyPem(), service.config.Now())
	if err != nil {
		return nil, err
	}
	now := service.config.Now().UTC()
	profile := Profile{
		ID: profileID, Name: strings.TrimSpace(request.GetName()), DNSNames: metadata.DNSNames,
		Fingerprint: metadata.Fingerprint, NotBefore: metadata.NotBefore, NotAfter: metadata.NotAfter, UpdatedAt: now,
	}

	service.secretState.Lock()
	profile, bindings, err := service.uploadProfileLocked(ctx, request, actorID, profile)
	service.secretState.Unlock()
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		service.push(ctx, binding)
	}
	profile.Bindings = bindings
	return &cloudv1.UploadCertificateProfileResponse{Profile: service.projectProfile(ctx, profile)}, nil
}

func (service *Service) uploadProfileLocked(ctx context.Context, request *cloudv1.UploadCertificateProfileRequest, actorID string, profile Profile) (Profile, []Binding, error) {
	if profile.ID == "" {
		profile.ID, profile.Revision, profile.CreatedAt = uuid.NewString(), 1, profile.UpdatedAt
	} else {
		current, err := service.config.Store.GetCertificateProfile(ctx, profile.ID)
		if err != nil {
			return Profile{}, nil, err
		}
		profile.Revision, profile.CreatedAt = current.Revision+1, current.CreatedAt
	}
	secretRef, err := service.config.Secrets.Put(request.GetCertificateChainPem(), request.GetPrivateKeyPem())
	if err != nil {
		return Profile{}, nil, err
	}
	profile.SecretRef = secretRef

	var bindings []Binding
	if strings.TrimSpace(request.GetCertificateProfileId()) == "" {
		err = service.config.Store.CreateCertificateProfile(ctx, profile, actorID)
	} else {
		bindings, err = service.config.Store.ReplaceCertificateProfile(ctx, request.GetExpectedRevision(), profile, actorID)
	}
	if err != nil {
		profiles, truthErr := service.config.Store.ListCertificateProfiles(ctx)
		if truthErr != nil {
			return Profile{}, nil, errors.Join(err, fmt.Errorf("read certificate truth after mutation failure: %w", truthErr))
		}
		references := make([]string, 0, len(profiles))
		for _, current := range profiles {
			references = append(references, current.SecretRef)
		}
		reconcileErr := service.config.Secrets.Reconcile(references)
		if committed, found := profileWithSecretRef(profiles, secretRef); found {
			if committed.ID != profile.ID {
				return Profile{}, nil, errors.Join(err, reconcileErr, errors.New("certificate secret reference belongs to an unexpected profile"))
			}
			if reconcileErr != nil {
				service.config.Logger.Error("certificate secret reconciliation deferred after ambiguous committed upload")
			}
			return committed, committed.Bindings, nil
		}
		return Profile{}, nil, errors.Join(err, reconcileErr)
	}
	if _, reconcileErr := service.listProfilesAndReconcileLocked(ctx); reconcileErr != nil {
		service.config.Logger.Error("certificate secret reconciliation deferred after committed upload")
	}
	return profile, bindings, nil
}

// ReconcileSecrets 把数据库中的当前 secret 引用与受管文件系统收敛到一致状态。
func (service *Service) ReconcileSecrets(ctx context.Context) error {
	service.secretState.Lock()
	defer service.secretState.Unlock()
	_, err := service.listProfilesAndReconcileLocked(ctx)
	return err
}

func (service *Service) listProfilesAndReconcileLocked(ctx context.Context) ([]Profile, error) {
	profiles, err := service.config.Store.ListCertificateProfiles(ctx)
	if err != nil {
		return nil, err
	}
	references := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		references = append(references, profile.SecretRef)
	}
	if err := service.config.Secrets.Reconcile(references); err != nil {
		return profiles, err
	}
	return profiles, nil
}

func profileWithSecretRef(profiles []Profile, secretRef string) (Profile, bool) {
	for _, profile := range profiles {
		if profile.SecretRef == secretRef {
			return profile, true
		}
	}
	return Profile{}, false
}

// BindProfile 使用 binding revision CAS 选择档案；空档案 ID 只解除后续自动更新。
func (service *Service) BindProfile(ctx context.Context, request *cloudv1.BindCertificateProfileRequest, actorID string) (*cloudv1.BindCertificateProfileResponse, error) {
	if request == nil || strings.TrimSpace(request.GetEdgeId()) == "" || strings.TrimSpace(actorID) == "" {
		return nil, errors.New("Edge ID and operator identity are required")
	}
	edge, err := service.config.Edges.GetEdge(ctx, request.GetEdgeId())
	if err != nil {
		return nil, err
	}
	now := service.config.Now().UTC()
	if strings.TrimSpace(request.GetCertificateProfileId()) == "" {
		binding, err := service.config.Store.UnbindCertificateProfile(ctx, edge.ID, request.GetExpectedBindingRevision(), actorID, now)
		return &cloudv1.BindCertificateProfileResponse{Binding: service.projectBinding(ctx, binding)}, err
	}
	profile, err := service.config.Store.GetCertificateProfile(ctx, request.GetCertificateProfileId())
	if err != nil {
		return nil, err
	}
	if err := VerifyEndpoint(profile, edge.PublicEndpoint); err != nil {
		return nil, err
	}
	binding, err := service.config.Store.BindCertificateProfile(ctx, edge, profile, request.GetExpectedBindingRevision(), actorID, now)
	if err != nil {
		return nil, err
	}
	service.push(ctx, binding)
	return &cloudv1.BindCertificateProfileResponse{Binding: service.projectBinding(ctx, binding)}, nil
}

// BundleForEdge 返回当前绑定的完整证书包，仅供已认证 EdgeControl 连接调用。
func (service *Service) BundleForEdge(ctx context.Context, edgeID string) (*cloudv1.EdgeCertificateBundle, error) {
	service.secretState.RLock()
	defer service.secretState.RUnlock()

	binding, found, err := service.config.Store.GetCertificateBinding(ctx, strings.TrimSpace(edgeID))
	if err != nil || !found {
		return nil, err
	}
	profile, err := service.config.Store.GetCertificateProfile(ctx, binding.ProfileID)
	if err != nil {
		return nil, err
	}
	certificatePEM, privateKeyPEM, err := service.config.Secrets.Read(profile.SecretRef)
	if err != nil {
		return nil, err
	}
	return bundle(binding, profile, certificatePEM, privateKeyPEM), nil
}

// BindingForEdge 返回 Edge 管理页面需要的证书状态；未绑定时返回 nil。
func (service *Service) BindingForEdge(ctx context.Context, edgeID string) (*cloudv1.CertificateBinding, error) {
	binding, found, err := service.config.Store.GetCertificateBinding(ctx, strings.TrimSpace(edgeID))
	if err != nil || !found {
		return nil, err
	}
	return service.projectBinding(ctx, binding), nil
}

// ValidateEdgeEndpoint 阻止已绑定 Edge 把公网域名改到当前证书 SAN 之外。
func (service *Service) ValidateEdgeEndpoint(ctx context.Context, edgeID, publicEndpoint string) error {
	binding, found, err := service.config.Store.GetCertificateBinding(ctx, strings.TrimSpace(edgeID))
	if err != nil || !found {
		return err
	}
	profile, err := service.config.Store.GetCertificateProfile(ctx, binding.ProfileID)
	if err != nil {
		return err
	}
	return VerifyEndpoint(profile, publicEndpoint)
}

// RecordApplied 持久化 Edge 对当前 desired revision 的回执；旧 profile/revision 回执不会覆盖新状态。
func (service *Service) RecordApplied(ctx context.Context, edgeID string, applied *cloudv1.CertificateApplied) error {
	if applied == nil || len(applied.GetErrorCode()) > 64 || len(applied.GetErrorMessage()) > 512 {
		return errors.New("certificate applied result exceeds the allowed error size")
	}
	return service.config.Store.RecordCertificateApplied(ctx, strings.TrimSpace(edgeID), applied, service.config.Now().UTC())
}

func (service *Service) push(ctx context.Context, binding Binding) {
	_ = service.config.Dispatcher.RefreshCertificate(ctx, binding.EdgeID)
}

func bundle(binding Binding, profile Profile, certificatePEM, privateKeyPEM []byte) *cloudv1.EdgeCertificateBundle {
	return &cloudv1.EdgeCertificateBundle{
		TargetEdgeId: binding.EdgeID, CertificateProfileId: profile.ID, Revision: profile.Revision,
		PublicEndpoint: binding.PublicEndpoint, CertificateChainPem: append([]byte(nil), certificatePEM...), PrivateKeyPem: append([]byte(nil), privateKeyPEM...),
	}
}

func (service *Service) projectProfile(ctx context.Context, profile Profile) *cloudv1.CertificateProfile {
	result := &cloudv1.CertificateProfile{
		CertificateProfileId: profile.ID, Name: profile.Name, DnsNames: append([]string(nil), profile.DNSNames...), Sha256Fingerprint: profile.Fingerprint,
		NotBefore: timestamppb.New(profile.NotBefore), NotAfter: timestamppb.New(profile.NotAfter), Revision: profile.Revision,
		CreatedAt: timestamppb.New(profile.CreatedAt), UpdatedAt: timestamppb.New(profile.UpdatedAt), Bindings: make([]*cloudv1.CertificateBinding, 0, len(profile.Bindings)),
	}
	for _, binding := range profile.Bindings {
		result.Bindings = append(result.Bindings, service.projectBinding(ctx, binding))
	}
	return result
}

func (service *Service) projectBinding(ctx context.Context, binding Binding) *cloudv1.CertificateBinding {
	result := &cloudv1.CertificateBinding{
		EdgeId: binding.EdgeID, EdgeName: binding.EdgeName, PublicEndpoint: binding.PublicEndpoint,
		CertificateProfileId: binding.ProfileID, CertificateProfileName: binding.ProfileName, BindingRevision: binding.BindingRevision,
		DesiredRevision: binding.DesiredRevision, AppliedRevision: binding.AppliedRevision,
		LastErrorCode: binding.LastErrorCode, LastErrorMessage: binding.LastErrorMessage,
	}
	if !binding.AppliedAt.IsZero() {
		result.AppliedAt = timestamppb.New(binding.AppliedAt)
	}
	if binding.ProfileID == "" || binding.DesiredRevision == 0 {
		return result
	}
	result.SyncState = cloudv1.CertificateSyncState_CERTIFICATE_SYNC_STATE_PENDING
	if binding.LastErrorCode != "" {
		result.SyncState = cloudv1.CertificateSyncState_CERTIFICATE_SYNC_STATE_FAILED
	} else if binding.AppliedProfileID == binding.ProfileID && binding.AppliedRevision == binding.DesiredRevision && binding.DesiredRevision != 0 {
		result.SyncState = cloudv1.CertificateSyncState_CERTIFICATE_SYNC_STATE_APPLIED
	}
	result.Online, _ = service.config.Online(ctx, binding.EdgeID)
	return result
}

// PairMetadata 是从证书内容推导出的持久非敏感字段。
type PairMetadata struct {
	DNSNames    []string
	Fingerprint string
	NotBefore   time.Time
	NotAfter    time.Time
}

// ValidatePair 校验证书链可解析、私钥匹配、DNS SAN 和当前有效期。
func ValidatePair(certificatePEM, privateKeyPEM []byte, now time.Time) (PairMetadata, error) {
	if len(certificatePEM) == 0 || len(privateKeyPEM) == 0 || len(certificatePEM) > maxPEMSize || len(privateKeyPEM) > maxPEMSize {
		return PairMetadata{}, errors.New("certificate chain and private key must be non-empty and at most 1 MiB each")
	}
	validated, err := securetransport.ValidateServerPair(certificatePEM, privateKeyPEM, "", now)
	if err != nil {
		return PairMetadata{}, err
	}
	leaf := validated.Leaf
	digest := sha256.Sum256(leaf.Raw)
	dnsNames := append([]string(nil), leaf.DNSNames...)
	for _, address := range leaf.IPAddresses {
		dnsNames = append(dnsNames, address.String())
	}
	return PairMetadata{DNSNames: dnsNames, Fingerprint: strings.ToUpper(hex.EncodeToString(digest[:])), NotBefore: leaf.NotBefore.UTC(), NotAfter: leaf.NotAfter.UTC()}, nil
}

// VerifyEndpoint 保证档案 SAN 覆盖 Edge 的公网域名或 IP 地址。
func VerifyEndpoint(profile Profile, publicEndpoint string) error {
	host := strings.TrimSpace(publicEndpoint)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = strings.Trim(parsed, "[]")
	}
	if address := net.ParseIP(host); address != nil {
		for _, identity := range profile.DNSNames {
			if candidate := net.ParseIP(identity); candidate != nil && candidate.Equal(address) {
				return nil
			}
		}
		return fmt.Errorf("certificate does not cover Edge public endpoint %q", host)
	}
	certificate := &x509.Certificate{DNSNames: profile.DNSNames}
	if err := certificate.VerifyHostname(host); err != nil {
		return fmt.Errorf("certificate does not cover Edge public endpoint %q: %w", host, err)
	}
	return nil
}
