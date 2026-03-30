package persist

import "errors"

var ErrLegacyImportUnimplemented = errors.New("legacy V1 import is not implemented")

// Phase 0 只占位。
// 实现时必须将 V1 的 workspaceStateFile / workspaceStateEntry 等 struct
// 复制到这里（不可 import 旧 tui/），并提供 V1 -> V2 单向转换函数。
func ImportV1(data []byte) (*WorkspaceStateFileV2, error) {
	_ = data
	return nil, ErrLegacyImportUnimplemented
}
