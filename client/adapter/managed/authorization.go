package managed

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
)

// CredentialSource 从当前平台 secure store 解析单个 endpoint-bound capability credential。
// 返回值只在 Go remote-auth adapter 内短暂使用；实现不得从其他 endpoint、Cloud token 或临时生成 key 回退。
type CredentialSource interface {
	// ResolveClientCredential 解析 endpoint/ref 对应的 grant 与 public identity projection。
	ResolveClientCredential(context.Context, string, string) (remoteauth.ClientAccessCredential, error)
}

// SignerSource 从 Android Keystore、WebCrypto 或其他平台 secure key owner 解析不可导出 signer。
// identity 只含 endpoint/public key/fingerprint 投影；实现必须返回同一 key 的 signer，remote-auth 会对签名结果再次验签。
type SignerSource interface {
	// ResolveClientSigner 返回与 identity public key 匹配的异步 signer；失败时不得生成替代 key。
	ResolveClientSigner(context.Context, string, string, remoteauth.ClientAccessIdentity) (remoteauth.ClientAccessSigner, error)
}

// CapabilityAuthorizer 使用共享 Go remote-auth 状态机完成 managed DataChannel 授权。
// Credentials 是平台 secure-store adapter；Random/Now 仅用于 deterministic harness，生产零值使用安全默认值。
type CapabilityAuthorizer struct {
	Credentials CredentialSource
	Signers     SignerSource
	Random      io.Reader
	Now         func() time.Time
}

// Prepare 在 Cloud signaling 之前验证 credential 的 endpoint、issuer、subject 和有效期。
// 成功结果冻结本次 attempt 使用的 credential，防止连接过程中 secure-store ref 被替换后混入其他身份。
func (authorizer CapabilityAuthorizer) Prepare(ctx context.Context, request clientruntime.AttemptRequest) (PreparedAuthorization, error) {
	if authorizer.Credentials == nil {
		return nil, fmt.Errorf("managed endpoint credential source is required")
	}
	route := request.Route()
	credential, err := authorizer.Credentials.ResolveClientCredential(ctx, string(request.EndpointID()), route.CredentialRef)
	if err != nil {
		return nil, err
	}
	var signer remoteauth.ClientAccessSigner
	if authorizer.Signers != nil {
		if err := credential.Identity.ValidatePublic(); err != nil {
			return nil, fmt.Errorf("managed endpoint ClientAccessIdentity public projection is invalid: %w", err)
		}
		signer, err = authorizer.Signers.ResolveClientSigner(ctx, string(request.EndpointID()), route.CredentialRef, credential.Identity)
		if err != nil {
			return nil, err
		}
		if signer == nil {
			return nil, fmt.Errorf("managed endpoint signer source returned no signer")
		}
	} else {
		signer, err = remoteauth.NewPrivateClientAccessSigner(credential.Identity)
		if err != nil {
			return nil, fmt.Errorf("managed endpoint ClientAccessIdentity is invalid: %w", err)
		}
	}
	endpointID := string(request.EndpointID())
	if strings.TrimSpace(credential.EndpointID) != endpointID || credential.Identity.EndpointID != endpointID {
		return nil, fmt.Errorf("managed endpoint credential belongs to endpoint %q, not %q", credential.EndpointID, endpointID)
	}
	if !credential.Ready() {
		return nil, fmt.Errorf("managed endpoint capability credential is awaiting pairing")
	}
	now := time.Now().UTC()
	if authorizer.Now != nil {
		now = authorizer.Now().UTC()
	}
	claims, err := remoteauth.Verify(credential.CapabilityGrant, request.DaemonIdentity().DeviceFingerprint, now, nil)
	if err != nil {
		return nil, fmt.Errorf("verify managed endpoint capability grant: %w", err)
	}
	if claims.IssuerDeviceID != request.DaemonIdentity().DeviceID {
		return nil, fmt.Errorf("managed endpoint device mismatch: grant %q registry %q", claims.IssuerDeviceID, request.DaemonIdentity().DeviceID)
	}
	if claims.SubjectKeyFingerprint != credential.Identity.Fingerprint {
		return nil, fmt.Errorf("managed endpoint capability subject does not match ClientAccessIdentity")
	}
	return &preparedCapabilityAuthorization{
		credential: credential, identity: request.DaemonIdentity(), signer: signer, random: authorizer.Random, now: authorizer.Now,
	}, nil
}

type preparedCapabilityAuthorization struct {
	credential remoteauth.ClientAccessCredential
	identity   endpoint.DaemonIdentity
	signer     remoteauth.ClientAccessSigner
	random     io.Reader
	now        func() time.Time
}

func (authorization *preparedCapabilityAuthorization) Authenticate(ctx context.Context, connection transport.Transport, certificateFingerprint string) (remoteauth.Claims, error) {
	binding, err := remoteauth.DTLSChannelBinding(certificateFingerprint)
	if err != nil {
		return remoteauth.Claims{}, fmt.Errorf("bind managed endpoint DTLS certificate: %w", err)
	}
	handshake := remoteauth.ClientHandshake{Random: authorization.random, Now: authorization.now}
	return handshake.Authenticate(ctx, connection, remoteauth.ClientHandshakeRequest{
		ExpectedDeviceID: authorization.identity.DeviceID, ExpectedDeviceFingerprint: authorization.identity.DeviceFingerprint,
		Credential: authorization.credential, Signer: authorization.signer, ChannelBinding: binding,
	})
}

var _ Authorizer = CapabilityAuthorizer{}
