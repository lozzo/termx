package account

import (
	"context"
	"crypto/sha256"
	"time"

	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

type identityContextKey struct{}

// Identity 是 transport 校验 session 后注入 context 的账号身份。
type Identity struct {
	Account             *cloudv1.AccountProfile
	Roles               []cloudv1.AccountRole
	SessionID           string
	RecentAuthExpiresAt time.Time
	CSRFDigest          [sha256.Size]byte
}

// HasRole 判断当前身份是否具备精确角色；admin 隐式拥有 operator 权限。
func (identity Identity) HasRole(role cloudv1.AccountRole) bool {
	for _, current := range identity.Roles {
		if current == role || current == cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN && role == cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR {
			return true
		}
	}
	return false
}

// ContextWithIdentity 把已验证身份附加到当前请求 context。
func ContextWithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

// IdentityFromContext 返回 transport 已验证的身份；缺失时调用方必须拒绝请求。
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok && identity.Account != nil && identity.SessionID != ""
}
