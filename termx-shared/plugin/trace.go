package plugin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	defaultMaxTraceDepth = 8
	defaultMaxActorPath  = 32
)

// MessageTrace 是贯穿 hook、plugin、action 和后续 hook 的因果链。
// 它由 host 生成和递增，插件只能携带 opaque TraceParent，不能伪造 Depth 或 ActorPath。
type MessageTrace struct {
	TraceID        string
	ParentEventID  string
	ParentActionID string
	OriginPluginID PluginID
	LastPluginID   PluginID
	ActorPath      []PluginID
	Depth          int
}

// Clone 返回 trace 的深拷贝。
// ActorPath 是循环防护状态，复制后才能安全追加当前插件。
func (trace MessageTrace) Clone() MessageTrace {
	out := trace
	out.ActorPath = append([]PluginID(nil), trace.ActorPath...)
	return out
}

// ContainsActor 判断某个插件是否已经出现在当前 trace actor path 中。
// hook router 用它实现默认 self-caused 和跨插件环路防护。
func (trace MessageTrace) ContainsActor(pluginID PluginID) bool {
	for _, actor := range trace.ActorPath {
		if actor == pluginID {
			return true
		}
	}
	return false
}

// TraceParent 是插件可携带的 opaque trace 引用。
// Token 由 host 签发，包含签名后的 trace 状态；外部 runner 不能自行构造可信 TraceParent。
type TraceParent struct {
	TraceID string
	Token   string
}

// TraceManagerConfig 描述 trace manager 的 host-enforced policy。
// MaxDepth 和 MaxActorPath 是循环防护预算，不能由插件扩大。
type TraceManagerConfig struct {
	SigningKey   []byte
	MaxDepth     int
	MaxActorPath int
}

// TraceManager 负责签发和验证 message trace。
// 它是 host-side 组件；runner 只能拿到 TraceParent，不能拿到 signing key。
type TraceManager struct {
	signingKey   []byte
	maxDepth     int
	maxActorPath int
}

type traceTokenPayload struct {
	Trace MessageTrace `json:"trace"`
}

var (
	// ErrInvalidTraceToken 表示 TraceParent token 无法验证。
	ErrInvalidTraceToken = errors.New("invalid trace token")
	// ErrTraceDepthExceeded 表示 trace 已超过 host 允许的最大 hook/action 深度。
	ErrTraceDepthExceeded = errors.New("trace depth exceeded")
	// ErrTraceActorPathExceeded 表示 actor path 超过 host 允许的最大长度。
	ErrTraceActorPathExceeded = errors.New("trace actor path exceeded")
)

// NewTraceManager 创建 host-side trace manager。
// signingKey 必须由 host 持有；为空会返回错误，避免测试之外的无签名 trace。
func NewTraceManager(config TraceManagerConfig) (*TraceManager, error) {
	if len(config.SigningKey) == 0 {
		return nil, fmt.Errorf("trace manager signing key is required")
	}
	maxDepth := config.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxTraceDepth
	}
	maxActorPath := config.MaxActorPath
	if maxActorPath <= 0 {
		maxActorPath = defaultMaxActorPath
	}
	return &TraceManager{
		signingKey:   append([]byte(nil), config.SigningKey...),
		maxDepth:     maxDepth,
		maxActorPath: maxActorPath,
	}, nil
}

// NewRootTrace 创建一条新的 host-root trace。
// 外部请求、用户输入或 daemon lifecycle 事件进入插件消息流时应由 host 调用本方法。
func (manager *TraceManager) NewRootTrace(origin PluginID) (MessageTrace, TraceParent, error) {
	traceID, err := randomTraceID()
	if err != nil {
		return MessageTrace{}, TraceParent{}, err
	}
	return manager.NewRootTraceWithID(traceID, origin)
}

// NewRootTraceWithID 用指定 trace id 创建 root trace。
// 它主要用于 harness 中构造稳定 trace；生产路径通常使用 NewRootTrace。
func (manager *TraceManager) NewRootTraceWithID(traceID string, origin PluginID) (MessageTrace, TraceParent, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return MessageTrace{}, TraceParent{}, fmt.Errorf("trace id is required")
	}
	trace := MessageTrace{
		TraceID:        traceID,
		OriginPluginID: origin,
		LastPluginID:   origin,
	}
	if origin != "" {
		trace.ActorPath = []PluginID{origin}
	}
	parent, err := manager.Parent(trace)
	if err != nil {
		return MessageTrace{}, TraceParent{}, err
	}
	return trace, parent, nil
}

// Parent 签发当前 trace 的 opaque parent token。
// 插件调用后续 action 时只能携带该 token，不能自行修改 Depth 或 ActorPath。
func (manager *TraceManager) Parent(trace MessageTrace) (TraceParent, error) {
	token, err := manager.sign(trace)
	if err != nil {
		return TraceParent{}, err
	}
	return TraceParent{TraceID: trace.TraceID, Token: token}, nil
}

// TraceFromParent 验证 opaque parent token 并取回 host 签发的 trace 状态。
// token 被篡改、trace id 不匹配或签名错误都会失败。
func (manager *TraceManager) TraceFromParent(parent TraceParent) (MessageTrace, error) {
	trace, err := manager.verify(parent.Token)
	if err != nil {
		return MessageTrace{}, err
	}
	if parent.TraceID == "" || parent.TraceID != trace.TraceID {
		return MessageTrace{}, ErrInvalidTraceToken
	}
	return trace, nil
}

// DeriveActionTrace 从 opaque parent 派生 action trace。
// actor 和 actionID 必须来自已认证 caller/session 与 action catalog，不能来自插件请求体。
func (manager *TraceManager) DeriveActionTrace(parent TraceParent, actor PluginID, actionID ActionID) (MessageTrace, TraceParent, error) {
	trace, err := manager.TraceFromParent(parent)
	if err != nil {
		return MessageTrace{}, TraceParent{}, err
	}
	next, err := manager.advance(trace, actor)
	if err != nil {
		return MessageTrace{}, TraceParent{}, err
	}
	next.ParentActionID = string(actionID)
	parentOut, err := manager.Parent(next)
	if err != nil {
		return MessageTrace{}, TraceParent{}, err
	}
	return next, parentOut, nil
}

// DeriveEventTrace 从 opaque parent 派生 hook event trace。
// eventID 必须由发布系统事件的 host 生成，插件不能发布 termx.* 系统事件。
func (manager *TraceManager) DeriveEventTrace(parent TraceParent, actor PluginID, eventID string) (MessageTrace, TraceParent, error) {
	trace, err := manager.TraceFromParent(parent)
	if err != nil {
		return MessageTrace{}, TraceParent{}, err
	}
	next, err := manager.advance(trace, actor)
	if err != nil {
		return MessageTrace{}, TraceParent{}, err
	}
	next.ParentEventID = eventID
	parentOut, err := manager.Parent(next)
	if err != nil {
		return MessageTrace{}, TraceParent{}, err
	}
	return next, parentOut, nil
}

func (manager *TraceManager) advance(trace MessageTrace, actor PluginID) (MessageTrace, error) {
	if trace.Depth >= manager.maxDepth {
		return MessageTrace{}, ErrTraceDepthExceeded
	}
	if len(trace.ActorPath) >= manager.maxActorPath {
		return MessageTrace{}, ErrTraceActorPathExceeded
	}
	next := trace.Clone()
	next.Depth++
	if next.OriginPluginID == "" {
		next.OriginPluginID = actor
	}
	next.LastPluginID = actor
	if actor != "" {
		next.ActorPath = append(next.ActorPath, actor)
	}
	return next, nil
}

func (manager *TraceManager) sign(trace MessageTrace) (string, error) {
	payload, err := json.Marshal(traceTokenPayload{Trace: trace.Clone()})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, manager.signingKey)
	_, _ = mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (manager *TraceManager) verify(token string) (MessageTrace, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return MessageTrace{}, ErrInvalidTraceToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return MessageTrace{}, ErrInvalidTraceToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return MessageTrace{}, ErrInvalidTraceToken
	}
	mac := hmac.New(sha256.New, manager.signingKey)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return MessageTrace{}, ErrInvalidTraceToken
	}
	var decoded traceTokenPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return MessageTrace{}, ErrInvalidTraceToken
	}
	if decoded.Trace.TraceID == "" {
		return MessageTrace{}, ErrInvalidTraceToken
	}
	return decoded.Trace.Clone(), nil
}

func randomTraceID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
