package history

import "errors"

// ErrHistoryInvalidMutation 表示 renderer 发出的 mutation batch 不满足
// HistoryStore 的领域约束。它只能用于新 renderer/store 边界，不能作为旧
// projector fallback 的兜底错误。
var ErrHistoryInvalidMutation = errors.New("invalid history mutation")

// ErrHistoryUnsupportedWindowMode 表示 history window 请求的分页方向不是
// authoritative store 支持的模式。调用方不能因此退回 TUI 本地 scrollback。
var ErrHistoryUnsupportedWindowMode = errors.New("unsupported history window mode")

// ErrHistoryRendererNotImplemented 表示 R319 已清掉旧 projector/store，但新的
// logical renderer 尚未接入。这个错误用于防止旧错误模型继续对外提供历史。
var ErrHistoryRendererNotImplemented = errors.New("history logical renderer not implemented")
