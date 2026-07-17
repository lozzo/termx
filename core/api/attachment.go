package api

// TerminalAttachSpec 是 daemon-local attachment 创建请求。
// SurfaceID/ViewID 是 consumer view identity，不表达 client endpoint session generation。
type TerminalAttachSpec struct {
	TerminalID   string
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

// ResizeOwnership 是 owning daemon 对当前 resize owner 的权威 projection。
type ResizeOwnership struct {
	OwnerAttachmentID string
	OwnerSurfaceID    string
	OwnerViewID       string
	Size              TerminalSize
	SizeLocked        bool
	Epoch             uint64
}

// ResizeControl 是 daemon 对 attachment resize 权限和 owner 的确认。
type ResizeControl struct {
	CanResize      bool
	Reason         string
	SizeLocked     bool
	SurfaceID      string
	OwnerSurfaceID string
	OwnerViewID    string
	Ownership      *ResizeOwnership
}

// TerminalAttachResult 返回 daemon channel 与 resize control。
// Channel 只在当前 protocol session 内有效，client runtime 必须把它绑定到 EndpointSessionStamp。
type TerminalAttachResult struct {
	Mode    string
	Channel uint16
	Control ResizeControl
	Size    TerminalSize
}

// TerminalDetachSpec 精确指定 daemon-local attachment identity。
type TerminalDetachSpec struct {
	TerminalID string
	Channel    uint16
	SurfaceID  string
	ViewID     string
}

// TerminalInputSpec 携带 daemon-local attachment input；失败后 consumer 不得隐式重放 Data。
type TerminalInputSpec struct {
	TerminalID string
	Channel    uint16
	SurfaceID  string
	ViewID     string
	Data       []byte
}

// TerminalResizeSpec 携带 daemon-local attachment resize intent。
type TerminalResizeSpec struct {
	TerminalID   string
	Channel      uint16
	Size         TerminalSize
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

// TerminalResizeResult 返回 daemon 最终尺寸和 resize ownership/control。
type TerminalResizeResult struct {
	Size    TerminalSize
	Resized bool
	Control ResizeControl
}
