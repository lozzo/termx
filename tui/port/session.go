package port

import (
	"context"
)

// SessionService 保存 TUI 自己的轻量交互 session，不拥有 client runtime 或 daemon terminal session。
type SessionService interface {
	Load(context.Context) (SessionSnapshot, error)
	Save(context.Context, SessionSnapshot) error
}

// SessionSnapshot 只保存 TUI 自己可恢复的轻量交互选择，不包含 daemon 或 client runtime session。
type SessionSnapshot struct {
	ActiveTerminalID string
}
