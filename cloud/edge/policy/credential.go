// Package policy 实现 Edge 对 Controller 签发 RelayLease 的本地收缩和临时 credential 派生。
package policy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CredentialDeriver 使用 Edge 进程内随机 secret 派生 session-specific TURN credential。
// secret 不落盘，所以 Edge 重启会使旧 credential 自然失效。
type CredentialDeriver struct {
	secret []byte
	urls   []string
}

// RelayAdmission 是 Runtime actor 为 TURN allocation 冻结的执行上限。
// Limiter 由同一 RelayLease 的所有 allocation 共享；Relay 数据层不读取 Controller、订单或账号表。
type RelayAdmission struct {
	LeaseID                  string
	SessionID                string
	MaxBytes                 uint64
	MaxRateBytesPerSecond    uint64
	MaxConcurrentAllocations uint32
	Limiter                  *AdmissionLimiter
}

// NewCredentialDeriver 要求至少 32 字节进程 secret 和至少一个版本化 TURN URL。
func NewCredentialDeriver(secret []byte, urls []string) (*CredentialDeriver, error) {
	if len(secret) < 32 || len(urls) == 0 {
		return nil, errors.New("TURN credential secret and URL are required")
	}
	cleanURLs := make([]string, 0, len(urls))
	for _, value := range urls {
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(value, "turn:") && !strings.HasPrefix(value, "turns:") {
			return nil, fmt.Errorf("TURN URL %q is invalid", value)
		}
		cleanURLs = append(cleanURLs, value)
	}
	return &CredentialDeriver{secret: append([]byte(nil), secret...), urls: cleanURLs}, nil
}

// Material 从已验证 RelayLease 生成只在当前信令 attempt 中返回的 ICE 参数。
func (deriver *CredentialDeriver) Material(claims *cloudv1.RelayLeaseClaims) (*cloudv1.RelayICEConfig, error) {
	if deriver == nil || claims == nil || claims.GetExpiresAt() == nil || strings.TrimSpace(claims.GetLeaseId()) == "" || strings.TrimSpace(claims.GetSessionId()) == "" {
		return nil, errors.New("verified RelayLease is required")
	}
	username := strconv.FormatInt(claims.GetExpiresAt().AsTime().Unix(), 10) + ":" + claims.GetLeaseId() + ":" + claims.GetSessionId()
	return &cloudv1.RelayICEConfig{
		LeaseId: claims.GetLeaseId(), Urls: append([]string(nil), deriver.urls...), Username: username, Credential: deriver.Password(username),
		ExpiresAt: timestamppb.New(claims.GetExpiresAt().AsTime()),
	}, nil
}

// Password 确定性派生 Pion long-term auth 使用的临时密码；调用方仍必须在 Runtime 验证 username 当前活跃。
func (deriver *CredentialDeriver) Password(username string) string {
	mac := hmac.New(sha256.New, deriver.secret)
	_, _ = mac.Write([]byte(username))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
