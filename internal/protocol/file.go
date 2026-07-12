package protocol

import (
	"time"

	"github.com/lozzow/termx/proto/wirepb"
	"google.golang.org/protobuf/proto"
)

// FilePathParams 表达 daemon 文件系统上的单路径 mutation。
// Path 必须是 owning daemon 机器上的绝对路径；Recursive 只对显式支持递归的操作生效。
type FilePathParams struct {
	Path      string
	Recursive bool
}

// FileListParams 表达 daemon-owned 目录分页请求。
// Cursor 只由 owning daemon 签发和解释，客户端不得从目录项推导下一页位置。
type FileListParams struct {
	Path   string
	Cursor string
	Limit  int
}

// FileEntry 是 daemon 文件系统 metadata 的只读协议投影。
// 它不持有文件内容；Type 取 file、dir、symlink 或 other。
type FileEntry struct {
	Path       string
	Name       string
	Type       string
	Size       int64
	Mode       uint32
	ModifiedAt time.Time
	LinkTarget string
}

// FileListResult 是单次目录枚举窗口。
// NextCursor 为空表示当前窗口结束；目录变化可让非空 cursor 在后续请求中失效。
type FileListResult struct {
	Path       string
	Entries    []FileEntry
	NextCursor string
}

// FilePreviewParams 请求有界读取普通文件前缀。
// MaxBytes 必须由 daemon 限制，客户端值不能扩大服务端上限。
type FilePreviewParams struct {
	Path     string
	MaxBytes int64
}

// FilePreviewResult 是有界内容预览，不是下载通道。
// Content 来自 owning daemon；Truncated 表示文件还有未返回内容。
type FilePreviewResult struct {
	Entry     FileEntry
	MIMEType  string
	Content   []byte
	Truncated bool
}

// FileRenameParams 表达单个原子重命名请求。
// Overwrite=false 时目标已存在必须失败，不能由实现静默覆盖。
type FileRenameParams struct {
	Path      string
	NewPath   string
	Overwrite bool
}

// FileCopyMoveParams 表达复制或移动到目标目录的批量请求。
// 每个源项必须独立返回结果，部分成功不能伪装成整体成功。
type FileCopyMoveParams struct {
	Paths     []string
	TargetDir string
	Overwrite bool
}

// FileOperationResult 表达一个文件 mutation 的确定结果。
// ErrorCode 是稳定协议分类，ErrorMessage 只用于用户诊断。
type FileOperationResult struct {
	Path         string
	TargetPath   string
	Success      bool
	ErrorCode    string
	ErrorMessage string
}

// FileBatchResult 保存批量 mutation 的逐项结果。
// Results 顺序与请求 Paths 一致，客户端不得因单项失败自动重放其它项。
type FileBatchResult struct {
	Results []FileOperationResult
}

func fileEntryToWirePB(entry FileEntry) *wirepb.FileEntry {
	return &wirepb.FileEntry{Path: entry.Path, Name: entry.Name, Type: entry.Type, Size: entry.Size, Mode: entry.Mode, ModifiedAtUnixNano: entry.ModifiedAt.UnixNano(), LinkTarget: entry.LinkTarget}
}

func fileEntryFromWirePB(entry *wirepb.FileEntry) FileEntry {
	if entry == nil {
		return FileEntry{}
	}
	return FileEntry{Path: entry.GetPath(), Name: entry.GetName(), Type: entry.GetType(), Size: entry.GetSize(), Mode: entry.GetMode(), ModifiedAt: time.Unix(0, entry.GetModifiedAtUnixNano()).UTC(), LinkTarget: entry.GetLinkTarget()}
}

func fileListResultToWirePB(result FileListResult) *wirepb.FileListResult {
	out := &wirepb.FileListResult{Path: result.Path, NextCursor: result.NextCursor, Entries: make([]*wirepb.FileEntry, 0, len(result.Entries))}
	for _, entry := range result.Entries {
		out.Entries = append(out.Entries, fileEntryToWirePB(entry))
	}
	return out
}

func fileListResultFromWirePB(result *wirepb.FileListResult) FileListResult {
	out := FileListResult{Path: result.GetPath(), NextCursor: result.GetNextCursor(), Entries: make([]FileEntry, 0, len(result.GetEntries()))}
	for _, entry := range result.GetEntries() {
		out.Entries = append(out.Entries, fileEntryFromWirePB(entry))
	}
	return out
}

func fileOperationResultToWirePB(result FileOperationResult) *wirepb.FileOperationResult {
	return &wirepb.FileOperationResult{Path: result.Path, TargetPath: result.TargetPath, Success: result.Success, ErrorCode: result.ErrorCode, ErrorMessage: result.ErrorMessage}
}

func fileOperationResultFromWirePB(result *wirepb.FileOperationResult) FileOperationResult {
	return FileOperationResult{Path: result.GetPath(), TargetPath: result.GetTargetPath(), Success: result.GetSuccess(), ErrorCode: result.GetErrorCode(), ErrorMessage: result.GetErrorMessage()}
}

func fileBatchResultToWirePB(result FileBatchResult) *wirepb.FileBatchResult {
	out := &wirepb.FileBatchResult{Results: make([]*wirepb.FileOperationResult, 0, len(result.Results))}
	for _, item := range result.Results {
		out.Results = append(out.Results, fileOperationResultToWirePB(item))
	}
	return out
}

func fileBatchResultFromWirePB(result *wirepb.FileBatchResult) FileBatchResult {
	out := FileBatchResult{Results: make([]FileOperationResult, 0, len(result.GetResults()))}
	for _, item := range result.GetResults() {
		out.Results = append(out.Results, fileOperationResultFromWirePB(item))
	}
	return out
}

func encodeFileMethodParams(method string, params any) ([]byte, bool, error) {
	message, handled, err := fileParamsToWirePB(method, params)
	if !handled || err != nil {
		return nil, handled, err
	}
	payload, err := proto.Marshal(message)
	return payload, true, err
}

func fileParamsToWirePB(method string, params any) (proto.Message, bool, error) {
	switch method {
	case "file.list":
		value, ok := params.(FileListParams)
		if !ok {
			return nil, true, methodParamsTypeError(method, "protocol.FileListParams", params)
		}
		return &wirepb.FileListParams{Path: value.Path, Cursor: value.Cursor, Limit: int32(value.Limit)}, true, nil
	case "file.stat", "file.mkdir", "file.delete":
		value, ok := params.(FilePathParams)
		if !ok {
			return nil, true, methodParamsTypeError(method, "protocol.FilePathParams", params)
		}
		return &wirepb.FilePathParams{Path: value.Path, Recursive: value.Recursive}, true, nil
	case "file.preview":
		value, ok := params.(FilePreviewParams)
		if !ok {
			return nil, true, methodParamsTypeError(method, "protocol.FilePreviewParams", params)
		}
		return &wirepb.FilePreviewParams{Path: value.Path, MaxBytes: value.MaxBytes}, true, nil
	case "file.rename":
		value, ok := params.(FileRenameParams)
		if !ok {
			return nil, true, methodParamsTypeError(method, "protocol.FileRenameParams", params)
		}
		return &wirepb.FileRenameParams{Path: value.Path, NewPath: value.NewPath, Overwrite: value.Overwrite}, true, nil
	case "file.copy", "file.move":
		value, ok := params.(FileCopyMoveParams)
		if !ok {
			return nil, true, methodParamsTypeError(method, "protocol.FileCopyMoveParams", params)
		}
		return &wirepb.FileCopyMoveParams{Paths: append([]string(nil), value.Paths...), TargetDir: value.TargetDir, Overwrite: value.Overwrite}, true, nil
	default:
		return nil, false, nil
	}
}

func decodeFileMethodParams(method string, payload []byte) (any, bool, error) {
	switch method {
	case "file.list":
		var msg wirepb.FileListParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, true, err
		}
		return FileListParams{Path: msg.GetPath(), Cursor: msg.GetCursor(), Limit: int(msg.GetLimit())}, true, nil
	case "file.stat", "file.mkdir", "file.delete":
		var msg wirepb.FilePathParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, true, err
		}
		return FilePathParams{Path: msg.GetPath(), Recursive: msg.GetRecursive()}, true, nil
	case "file.preview":
		var msg wirepb.FilePreviewParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, true, err
		}
		return FilePreviewParams{Path: msg.GetPath(), MaxBytes: msg.GetMaxBytes()}, true, nil
	case "file.rename":
		var msg wirepb.FileRenameParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, true, err
		}
		return FileRenameParams{Path: msg.GetPath(), NewPath: msg.GetNewPath(), Overwrite: msg.GetOverwrite()}, true, nil
	case "file.copy", "file.move":
		var msg wirepb.FileCopyMoveParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, true, err
		}
		return FileCopyMoveParams{Paths: append([]string(nil), msg.GetPaths()...), TargetDir: msg.GetTargetDir(), Overwrite: msg.GetOverwrite()}, true, nil
	default:
		return nil, false, nil
	}
}

func encodeFileMethodResult(method string, result any) ([]byte, bool, error) {
	var message proto.Message
	switch method {
	case "file.list":
		value, ok := result.(FileListResult)
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.FileListResult", result)
		}
		message = fileListResultToWirePB(value)
	case "file.stat":
		value, ok := result.(FileEntry)
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.FileEntry", result)
		}
		message = fileEntryToWirePB(value)
	case "file.preview":
		value, ok := result.(FilePreviewResult)
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.FilePreviewResult", result)
		}
		message = &wirepb.FilePreviewResult{Entry: fileEntryToWirePB(value.Entry), MimeType: value.MIMEType, Content: append([]byte(nil), value.Content...), Truncated: value.Truncated}
	case "file.mkdir", "file.rename", "file.delete":
		value, ok := result.(FileOperationResult)
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.FileOperationResult", result)
		}
		message = fileOperationResultToWirePB(value)
	case "file.copy", "file.move":
		value, ok := result.(FileBatchResult)
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.FileBatchResult", result)
		}
		message = fileBatchResultToWirePB(value)
	default:
		return nil, false, nil
	}
	payload, err := proto.Marshal(message)
	return payload, true, err
}

func decodeFileMethodResult(method string, payload []byte, out any) (bool, error) {
	switch method {
	case "file.list":
		var msg wirepb.FileListResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return true, err
		}
		ptr, ok := out.(*FileListResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.FileListResult", out)
		}
		*ptr = fileListResultFromWirePB(&msg)
	case "file.stat":
		var msg wirepb.FileEntry
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return true, err
		}
		ptr, ok := out.(*FileEntry)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.FileEntry", out)
		}
		*ptr = fileEntryFromWirePB(&msg)
	case "file.preview":
		var msg wirepb.FilePreviewResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return true, err
		}
		ptr, ok := out.(*FilePreviewResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.FilePreviewResult", out)
		}
		*ptr = FilePreviewResult{Entry: fileEntryFromWirePB(msg.GetEntry()), MIMEType: msg.GetMimeType(), Content: append([]byte(nil), msg.GetContent()...), Truncated: msg.GetTruncated()}
	case "file.mkdir", "file.rename", "file.delete":
		var msg wirepb.FileOperationResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return true, err
		}
		ptr, ok := out.(*FileOperationResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.FileOperationResult", out)
		}
		*ptr = fileOperationResultFromWirePB(&msg)
	case "file.copy", "file.move":
		var msg wirepb.FileBatchResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return true, err
		}
		ptr, ok := out.(*FileBatchResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.FileBatchResult", out)
		}
		*ptr = fileBatchResultFromWirePB(&msg)
	default:
		return false, nil
	}
	return true, nil
}
