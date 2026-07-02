package protocol

import (
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var compactASCIIStrings = buildCompactASCIIStrings()

func buildCompactASCIIStrings() [128]string {
	var out [128]string
	for i := 0; i < len(out); i++ {
		out[i] = string(byte(i))
	}
	return out
}

type Hello struct {
	Version int
	Client  string
	Server  string
}

type Request struct {
	ID     uint64
	Method string
	Params []byte
}

type Response struct {
	ID     uint64
	Result []byte
}

type ProtocolError struct {
	Code    int
	Message string
}

type ErrorMessage struct {
	ID    uint64
	Error ProtocolError
}

type Size struct {
	Cols uint16
	Rows uint16
}

type TerminalInfo struct {
	ID                         string
	Name                       string
	Command                    []string
	Tags                       map[string]string
	Size                       Size
	State                      string
	CWD                        string
	LiveCWD                    string
	CreatedAt                  time.Time
	ExitCode                   *int
	ExitedAt                   time.Time
	ResizeOwnership            *ResizeOwnership
	ResizeOwnerAttachmentCount int
}

type CreateParams struct {
	Command            []string
	ID                 string
	Name               string
	Tags               map[string]string
	Size               Size
	Dir                string
	Env                []string
	ScrollbackSize     int
	ScrollbackMaxBytes int64
	ScrollbackMaxAge   time.Duration
}

type CreateResult struct {
	TerminalID string
	State      string
}

// Remote* 是 core-v2 daemon 暴露给 CLI/App 的显式 remote domain contract。
// wirepb 只作为跨进程编码格式，不能泄露成调用方依赖的业务类型。
type RemoteStatus struct {
	State         string
	Detail        string
	DeviceID      string
	DeviceName    string
	ControlURL    string
	HubURL        string
	HubURLs       []string
	DataDir       string
	Mode          string
	AllowLAN      bool
	TerminalCount int
	UpdatedAt     time.Time
}

type RemotePairStartParams struct {
	LocalPairURL   string
	TTLSeconds     int
	AuthTTLSeconds int
}

type RemotePairStartResult struct {
	Type              string
	MachineID         string
	MachineName       string
	LocalPairURL      string
	PairSessionID     string
	PairSecret        string
	AnswerProofSecret string
	ExpiresAt         time.Time
}

type RemoteLocalEnableParams struct {
	LocalWebAddr string
	ICETCPAddr   string
	HubURLs      []string
	ControlURL   string
	AccessToken  string
	Region       string
}

type RemoteLocalStatus struct {
	Enabled       bool
	HTTPURL       string
	LocalWebAddr  string
	LocalPairURL  string
	ICETCPEnabled bool
	ICETCPAddr    string
	ICETCPPort    int
	UpdatedAt     time.Time
}

type GetParams struct {
	TerminalID string
}

type ResizeParams struct {
	TerminalID string
	Cols       uint16
	Rows       uint16
}

// InputParams 是带 ack 的 terminal 输入请求；TUI-v3 用它校验当前 view attachment。
type InputParams struct {
	TerminalID string
	Channel    uint16
	SurfaceID  string
	ViewID     string
	Data       []byte
}

type EnsureResizeParams struct {
	TerminalID   string
	Channel      uint16
	Cols         uint16
	Rows         uint16
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type EnsureResizeResult struct {
	ResizeControl *ResizeControl
	Size          Size
	Resized       bool
}

type ResizeControlParams struct {
	TerminalID   string
	Channel      uint16
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type ResizeControlResult struct {
	ResizeControl *ResizeControl
	Size          Size
}

type SetTagsParams struct {
	TerminalID string
	Tags       map[string]string
}

type SetMetadataParams struct {
	TerminalID string
	Name       string
	Tags       map[string]string
}

type AttachParams struct {
	TerminalID   string
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type AttachResult struct {
	Mode          string
	Channel       uint16
	ResizeControl *ResizeControl
}

type ResizeOwnership struct {
	OwnerAttachmentID string
	OwnerSurfaceID    string
	OwnerViewID       string
	OwnerRemoteAddr   string
	Size              Size
	SizeLocked        bool
	Epoch             uint64
}

const (
	ResizePolicyOwner    = "owner"
	ResizePolicyFollower = "follower"
	ResizePolicyObserver = "observer"
)

const (
	ResizeControlReasonOwner      = "owner"
	ResizeControlReasonFollower   = "follower"
	ResizeControlReasonObserver   = "observer"
	ResizeControlReasonSizeLocked = "size_locked"
)

type ResizeControl struct {
	CanResize       bool
	Reason          string
	SizeLocked      bool
	SurfaceID       string
	OwnerSurfaceID  string
	OwnerViewID     string
	ResizeOwnership *ResizeOwnership
}

type EventType int

const (
	EventTerminalCreated         EventType = 1
	EventTerminalStateChanged    EventType = 2
	EventTerminalResized         EventType = 3
	EventTerminalRemoved         EventType = 4
	EventCollaboratorsRevoked    EventType = 5
	EventTerminalReadError       EventType = 6
	EventTerminalLiveInvalidated EventType = 7
	EventTerminalMetadataChanged EventType = 10
	EventStorageChanged          EventType = 11
	EventWorkbenchChanged        EventType = 12
)

type TerminalCreatedData struct {
	Name    string
	Command []string
	Size    Size
}

type TerminalStateChangedData struct {
	OldState string
	NewState string
	ExitCode *int
	ExitedAt time.Time
}

type TerminalResizedData struct {
	OldSize Size
	NewSize Size
}

type TerminalRemovedData struct {
	Reason string
}

type CollaboratorsRevokedData struct{}

type TerminalReadErrorData struct {
	Error string
}

// LiveScreenInvalidatedData 是 core native screen 的 latest-only 唤醒事件。
// 它不携带 screen rows；客户端收到后应按需调用 live.screen.get 拉取当前最新 NativeScreenSnapshot。
type LiveScreenInvalidatedData struct {
	Revision uint64
}

type StorageScope string

const (
	StorageScopePublic  StorageScope = "public"
	StorageScopePrivate StorageScope = "private"
)

type StorageEntry struct {
	AppID     string
	Scope     StorageScope
	OwnerID   string
	Key       string
	Value     []byte
	Version   uint64
	UpdatedAt time.Time
}

type StorageGetParams struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Key     string
}

type StoragePutParams struct {
	AppID           string
	Scope           StorageScope
	OwnerID         string
	Key             string
	Value           []byte
	CheckVersion    bool
	ExpectedVersion uint64
}

type StorageDeleteParams struct {
	AppID           string
	Scope           StorageScope
	OwnerID         string
	Key             string
	CheckVersion    bool
	ExpectedVersion uint64
}

type StorageDeleteResult struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Key     string
	Deleted bool
	Version uint64
}

type StorageListParams struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Prefix  string
}

type StorageListResult struct {
	Entries []StorageEntry
}

type StorageChangedData struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Key     string
	Version uint64
	Op      string
}

type WorkbenchChangedData struct {
	WorkspaceID string
	Version     uint64
	Action      string
	ResourceID  string
}

type Event struct {
	Type                 EventType
	TerminalID           string
	Timestamp            time.Time
	Created              *TerminalCreatedData
	StateChanged         *TerminalStateChangedData
	Resized              *TerminalResizedData
	Removed              *TerminalRemovedData
	CollaboratorsRevoked *CollaboratorsRevokedData
	ReadError            *TerminalReadErrorData
	LiveInvalidated      *LiveScreenInvalidatedData
	Storage              *StorageChangedData
	Workbench            *WorkbenchChangedData
}

type DetachParams struct {
	TerminalID string
	Channel    uint16
	SurfaceID  string
	ViewID     string
}

type EventsParams struct {
	TerminalID       string
	Types            []EventType
	StorageAppID     string
	StorageScope     StorageScope
	StorageOwnerID   string
	StorageKeyPrefix string
	WorkbenchID      string
}

type WorkbenchPaneKind string

const (
	WorkbenchPaneEmpty        WorkbenchPaneKind = "empty"
	WorkbenchPaneTerminalLive WorkbenchPaneKind = "terminal-live"
	WorkbenchPaneCopyHistory  WorkbenchPaneKind = "copy-history"
	WorkbenchPaneExited       WorkbenchPaneKind = "exited"
)

type WorkbenchSplitDirection string

const (
	WorkbenchSplitHorizontal WorkbenchSplitDirection = "horizontal"
	WorkbenchSplitVertical   WorkbenchSplitDirection = "vertical"
)

type WorkbenchSnapshot struct {
	Version           uint64
	ActiveWorkspaceID string
	Workspaces        []WorkbenchWorkspace
}

type WorkbenchWorkspace struct {
	ID          string
	Name        string
	ActiveTabID string
	Tabs        []WorkbenchTab
}

type WorkbenchTab struct {
	ID           string
	Title        string
	ActivePaneID string
	Panes        []WorkbenchPane
	RootSplit    WorkbenchSplitNode
}

type WorkbenchPane struct {
	ID         string
	Title      string
	Kind       WorkbenchPaneKind
	TerminalID string
}

type WorkbenchSplitNode struct {
	PaneID      string
	Direction   WorkbenchSplitDirection
	Children    []WorkbenchSplitNode
	Ratio       float64
	BiasCells   int
	FixedPaneID string
	FixedCols   int
	FixedRows   int
}

type WorkbenchMutationAction string

const (
	WorkbenchMutationWorkspaceCreate  WorkbenchMutationAction = "workspace.create"
	WorkbenchMutationWorkspaceRename  WorkbenchMutationAction = "workspace.rename"
	WorkbenchMutationWorkspaceDelete  WorkbenchMutationAction = "workspace.delete"
	WorkbenchMutationWorkspaceFocus   WorkbenchMutationAction = "workspace.focus"
	WorkbenchMutationTabCreate        WorkbenchMutationAction = "tab.create"
	WorkbenchMutationTabRename        WorkbenchMutationAction = "tab.rename"
	WorkbenchMutationTabDelete        WorkbenchMutationAction = "tab.delete"
	WorkbenchMutationTabFocus         WorkbenchMutationAction = "tab.focus"
	WorkbenchMutationPaneCreate       WorkbenchMutationAction = "pane.create"
	WorkbenchMutationPaneRename       WorkbenchMutationAction = "pane.rename"
	WorkbenchMutationPaneDelete       WorkbenchMutationAction = "pane.delete"
	WorkbenchMutationPaneFocus        WorkbenchMutationAction = "pane.focus"
	WorkbenchMutationPaneSplit        WorkbenchMutationAction = "pane.split"
	WorkbenchMutationPaneBindTerminal WorkbenchMutationAction = "pane.bind-terminal"
)

type WorkbenchGetParams struct {
	WorkspaceID string
}

type WorkbenchMutateParams struct {
	Action          WorkbenchMutationAction
	WorkspaceID     string
	TabID           string
	PaneID          string
	TargetID        string
	Name            string
	Kind            WorkbenchPaneKind
	TerminalID      string
	SplitDirection  WorkbenchSplitDirection
	CheckVersion    bool
	ExpectedVersion uint64
}

type WorkbenchMutateResult struct {
	Snapshot   WorkbenchSnapshot
	Action     WorkbenchMutationAction
	ResourceID string
}

// LiveScreenParams 是 realtime native screen 的请求参数。
// 它不包含 scrollback/page/window 字段；live.screen.get 总是返回 core 当前 latest screen。
type LiveScreenParams struct {
	TerminalID string
}

// LiveInvalidationNextParams 是 live display one-shot invalidation 的请求参数。
// ObservedRevision 只表示客户端上次从 core live.screen.get 或 wake 看到的
// native screen revision，用来补 one-shot arm 间隙丢失的 invalidation 边沿；
// 它不是 rendered revision，core 不能据此推断 TUI 已经写到哪一帧。
type LiveInvalidationNextParams struct {
	TerminalID       string
	ObservedRevision uint64
}

// HistoryWindowParams 是 authoritative history path 的请求参数。
// 它只表达 terminal-scoped history projection，不携带 pane/view/attachment
// identity；客户端若要把 response 重新绑定回本地 pane/view，只能依赖本地
// pending request 和 token/generation/logical cursor/logical boundary 命中后回填。
// 旧 BeforeOffset 保留为兼容字段；v3 应优先使用 token/generation/logical cursor
// 与 logical line boundary 做 stale guard。
type HistoryWindowParams struct {
	TerminalID          string
	BeforeOffset        int
	Limit               int
	Cols                int
	Mode                string
	Token               string
	Generation          uint64
	CursorValid         bool
	BeforeLineID        uint64
	BeforeRowInLine     int
	BeforeRowIndex      int
	CursorSegment       string
	AfterCursorValid    bool
	AfterLineID         uint64
	AfterRowInLine      int
	AfterRowIndex       int
	AfterCursorSegment  string
	BoundaryFirstLineID uint64
	BoundaryLastLineID  uint64
	RangeValid          bool
	RangeStartLineID    uint64
	RangeStartCol       int
	RangeEndLineID      uint64
	RangeEndCol         int
}

type ListResult struct {
	Terminals []TerminalInfo
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func encodeTerminalModesMask(modes TerminalModes) uint16 {
	var mask uint16
	if modes.AlternateScreen {
		mask |= 1 << 0
	}
	if modes.AlternateScroll {
		mask |= 1 << 1
	}
	if modes.MouseTracking {
		mask |= 1 << 2
	}
	if modes.MouseX10 {
		mask |= 1 << 3
	}
	if modes.MouseNormal {
		mask |= 1 << 4
	}
	if modes.MouseButtonEvent {
		mask |= 1 << 5
	}
	if modes.MouseAnyEvent {
		mask |= 1 << 6
	}
	if modes.MouseSGR {
		mask |= 1 << 7
	}
	if modes.BracketedPaste {
		mask |= 1 << 8
	}
	if modes.ApplicationCursor {
		mask |= 1 << 9
	}
	if modes.AutoWrap {
		mask |= 1 << 10
	}
	return mask
}

func decodeTerminalModesMask(mask uint16) TerminalModes {
	return TerminalModes{
		AlternateScreen:   mask&(1<<0) != 0,
		AlternateScroll:   mask&(1<<1) != 0,
		MouseTracking:     mask&(1<<2) != 0,
		MouseX10:          mask&(1<<3) != 0,
		MouseNormal:       mask&(1<<4) != 0,
		MouseButtonEvent:  mask&(1<<5) != 0,
		MouseAnyEvent:     mask&(1<<6) != 0,
		MouseSGR:          mask&(1<<7) != 0,
		BracketedPaste:    mask&(1<<8) != 0,
		ApplicationCursor: mask&(1<<9) != 0,
		AutoWrap:          mask&(1<<10) != 0,
	}
}

func encodeCursorShape(shape string) byte {
	switch shape {
	case "underline":
		return 1
	case "bar":
		return 2
	default:
		return 0
	}
}

func decodeCursorShape(shape byte) string {
	switch shape {
	case 1:
		return "underline"
	case 2:
		return "bar"
	default:
		return "block"
	}
}

type Cell struct {
	Content    string
	Width      int
	Style      CellStyle
	LinkURL    string
	LinkParams string
}

type CellStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

type CursorState struct {
	Row     int
	Col     int
	Visible bool
	Shape   string
	Blink   bool
}

type TerminalModes struct {
	AlternateScreen   bool
	AlternateScroll   bool
	MouseTracking     bool
	MouseX10          bool
	MouseNormal       bool
	MouseButtonEvent  bool
	MouseAnyEvent     bool
	MouseSGR          bool
	BracketedPaste    bool
	ApplicationCursor bool
	AutoWrap          bool
}

const (
	RowOwnershipPersisted         = "persisted"
	RowOwnershipLiveTailReclaimed = "live-tail-reclaimed"
	RowOwnershipLiveTailLive      = "live-tail-live"
	RowOwnershipScreen            = "screen"
)

// NativeScreenSnapshot 是 v3 live display 的专用协议投影。
// 它只表达 core 当前 native screen，不包含 scrollback、history generation、older cursor 或 copy/history token。
type NativeScreenSnapshot struct {
	TerminalID string
	Revision   uint64
	Size       Size
	Rows       []CompactRow
	AltScreen  bool
	Cursor     CursorState
	Modes      TerminalModes
	Timestamp  time.Time
}

type HistoryWindowOp string

const (
	HistoryWindowReplace HistoryWindowOp = "replace"
	HistoryWindowPrepend HistoryWindowOp = "prepend"
	HistoryWindowAppend  HistoryWindowOp = "append"
)

const (
	HistoryCursorSegmentCommitted            = "committed"
	HistoryCursorSegmentCurrentPrimaryFrame  = "current-primary-frame"
	HistoryCursorSegmentArchivedPrimaryFrame = "archived-primary-frame"
	HistoryCursorSegmentCurrentAltFrame      = "current-alt-frame"
)

type HistoryLineSpan struct {
	StartRow       int
	EndRow         int
	RowKind        string
	LogicalLineID  uint64
	SessionID      uint64
	FrameID        uint64
	FixedGrid      bool
	ScreenCols     int
	TimestampStart time.Time
	TimestampEnd   time.Time
	ClippedBefore  bool
	ClippedAfter   bool
}

// HistoryWindow 是 terminal-scoped authoritative history payload。
// 它只表达 logical line 在当前 cols 下的 history projection truth，不回显
// pane/view/workspace truth，也不携带 resize ownership 或 live attachment control。
// stale guard 只能依赖 token/generation/cursor/logical boundary；LoadedRows、
// TotalRows、BeforeOffset 等字段只能作为展示或兼容信息，不能替代这些 guard。
type HistoryWindow struct {
	TerminalID      string
	Token           string
	Op              HistoryWindowOp
	Size            Size
	Rows            []CompactRow
	RowTimestamps   []time.Time
	RowKinds        []string
	RowWrapped      []bool
	RowOwnership    []string
	RowSegments     []string
	RowSessionIDs   []uint64
	RowFrameIDs     []uint64
	RowFixedGrid    []bool
	RowScreenCols   []int
	RowScreenRows   []int
	RowScreenRowSet []bool
	RowIndexes      []int
	Lines           []HistoryLineSpan
	BeforeOffset    int
	LoadedRows      int
	TotalRows       int
	LoadedLines     int
	LogicalTotal    int
	HasMore         bool
	Generation      uint64
	FirstRowID      uint64
	LastRowID       uint64
	FirstLineID     uint64
	LastLineID      uint64
	CursorValid     bool
	CursorLineID    uint64
	CursorRow       int
	CursorRowIndex  int
	CursorSegment   string
	RowLineIDs      []uint64
	RowInLine       []int
	Timestamp       time.Time
}

func (s CellStyle) isZero() bool {
	return s == CellStyle{}
}

type CompactRowStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

type CompactRowCell struct {
	Content    string
	Width      int
	Style      *CompactRowStyle
	LinkURL    string
	LinkParams string
}

type CompactRowRun struct {
	Text       string
	Style      *CompactRowStyle
	LinkURL    string
	LinkParams string
}

type CompactRow struct {
	Text     string
	Runs     []CompactRowRun
	Cells    []CompactRowCell
	TailFill *CompactRowStyle
}

func (r CompactRow) DecodeCells() []Cell {
	return decodeCompactRow(r.Text, r.Runs, r.Cells)
}

func CompactRowsToCells(rows []CompactRow) [][]Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]Cell, len(rows))
	for i, row := range rows {
		out[i] = row.DecodeCells()
	}
	return out
}

func CompactRowsFromCells(rows [][]Cell) []CompactRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]CompactRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, CompactRowFromCells(row))
	}
	return out
}

func CompactRowsFromCellsPreserveTrailingBlankRows(rows [][]Cell) []CompactRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]CompactRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, CompactRowFromCellsPreserveTrailingBlankCells(row, true))
	}
	return out
}

func CompactRowFromCells(row []Cell) CompactRow {
	return CompactRowFromCellsPreserveTrailingBlankCells(row, false)
}

func BuildCompactRow(cellCount int, cellAt func(int) Cell) CompactRow {
	if cellCount <= 0 || cellAt == nil {
		return CompactRow{}
	}
	last := cellCount
	for last > 0 {
		cell := cellAt(last - 1)
		if cell.Content != "" && strings.TrimSpace(cell.Content) != "" {
			break
		}
		if !cell.Style.isZero() {
			break
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			break
		}
		last--
	}
	return buildCompactRow(last, cellAt)
}

func BuildCompactRowPreserveTrailingBlankCells(cellCount int, cellAt func(int) Cell) CompactRow {
	if cellCount <= 0 || cellAt == nil {
		return CompactRow{}
	}
	return buildCompactRow(cellCount, cellAt)
}

func CompactRowFromCellsPreserveTrailingBlankCells(row []Cell, preserveTrailingBlankCells bool) CompactRow {
	last := len(row)
	if !preserveTrailingBlankCells {
		for last > 0 {
			cell := row[last-1]
			if cell.Content != "" && strings.TrimSpace(cell.Content) != "" {
				break
			}
			if !cell.Style.isZero() {
				break
			}
			if cell.LinkURL != "" || cell.LinkParams != "" {
				break
			}
			last--
		}
	}
	row = row[:last]
	if len(row) == 0 {
		return CompactRow{}
	}
	return buildCompactRow(len(row), func(index int) Cell {
		return row[index]
	})
}

func buildCompactRow(count int, cellAt func(int) Cell) CompactRow {
	if count <= 0 {
		return CompactRow{}
	}
	var text strings.Builder
	allSimple := true
	allPlain := true
	for i := 0; i < count; i++ {
		cell := cellAt(i)
		cellText, ok := compactCellText(cell)
		if !ok {
			allSimple = false
			break
		}
		if !cell.Style.isZero() {
			allPlain = false
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			allPlain = false
		}
		text.WriteString(cellText)
	}
	if allSimple && allPlain {
		return CompactRow{Text: text.String()}
	}
	if allSimple {
		runs := make([]CompactRowRun, 0, 4)
		var runText strings.Builder
		first := cellAt(0)
		runStyle := first.Style
		runLinkURL := first.LinkURL
		runLinkParams := first.LinkParams
		flushRun := func() {
			if runText.Len() == 0 {
				return
			}
			runs = append(runs, CompactRowRun{
				Text:       runText.String(),
				Style:      compactRowStyleFromCellStyle(runStyle),
				LinkURL:    runLinkURL,
				LinkParams: runLinkParams,
			})
			runText.Reset()
		}
		for i := 0; i < count; i++ {
			cell := cellAt(i)
			if cell.Style != runStyle || cell.LinkURL != runLinkURL || cell.LinkParams != runLinkParams {
				flushRun()
				runStyle = cell.Style
				runLinkURL = cell.LinkURL
				runLinkParams = cell.LinkParams
			}
			cellText, _ := compactCellText(cell)
			runText.WriteString(cellText)
		}
		flushRun()
		return CompactRow{Runs: runs}
	}
	cells := make([]CompactRowCell, 0, count)
	for i := 0; i < count; i++ {
		cell := cellAt(i)
		cells = append(cells, CompactRowCell{
			Content:    cell.Content,
			Width:      compactCellWidth(cell),
			Style:      compactRowStyleFromCellStyle(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		})
	}
	return CompactRow{Cells: cells}
}

func CloneCompactRows(rows []CompactRow) []CompactRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]CompactRow, len(rows))
	for i, row := range rows {
		out[i] = CloneCompactRow(row)
	}
	return out
}

func CloneCompactRow(row CompactRow) CompactRow {
	cloned := CompactRow{Text: row.Text, TailFill: cloneCompactRowStyle(row.TailFill)}
	if len(row.Runs) > 0 {
		cloned.Runs = make([]CompactRowRun, len(row.Runs))
		for i, run := range row.Runs {
			cloned.Runs[i] = CompactRowRun{Text: run.Text, Style: cloneCompactRowStyle(run.Style), LinkURL: run.LinkURL, LinkParams: run.LinkParams}
		}
	}
	if len(row.Cells) > 0 {
		cloned.Cells = make([]CompactRowCell, len(row.Cells))
		for i, cell := range row.Cells {
			cloned.Cells[i] = CompactRowCell{Content: cell.Content, Width: cell.Width, Style: cloneCompactRowStyle(cell.Style), LinkURL: cell.LinkURL, LinkParams: cell.LinkParams}
		}
	}
	return cloned
}

func CompactRowEqual(left, right CompactRow) bool {
	if left.Text != right.Text || len(left.Runs) != len(right.Runs) || len(left.Cells) != len(right.Cells) || !compactRowStyleEqual(left.TailFill, right.TailFill) {
		return false
	}
	for i := range left.Runs {
		if left.Runs[i].Text != right.Runs[i].Text || left.Runs[i].LinkURL != right.Runs[i].LinkURL || left.Runs[i].LinkParams != right.Runs[i].LinkParams || !compactRowStyleEqual(left.Runs[i].Style, right.Runs[i].Style) {
			return false
		}
	}
	for i := range left.Cells {
		if left.Cells[i].Content != right.Cells[i].Content || left.Cells[i].Width != right.Cells[i].Width || left.Cells[i].LinkURL != right.Cells[i].LinkURL || left.Cells[i].LinkParams != right.Cells[i].LinkParams || !compactRowStyleEqual(left.Cells[i].Style, right.Cells[i].Style) {
			return false
		}
	}
	return true
}

func compactCellText(cell Cell) (string, bool) {
	if cell.Width > 1 {
		return "", false
	}
	if cell.Content == "" {
		return " ", true
	}
	if utf8.RuneCountInString(cell.Content) != 1 {
		return "", false
	}
	return cell.Content, true
}

func compactCellWidth(cell Cell) int {
	if cell.Width > 1 {
		return cell.Width
	}
	return 0
}

func compactRowStyleFromCellStyle(style CellStyle) *CompactRowStyle {
	if style.isZero() {
		return nil
	}
	return &CompactRowStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func cloneCompactRowStyle(style *CompactRowStyle) *CompactRowStyle {
	if style == nil {
		return nil
	}
	cloned := *style
	return &cloned
}

func compactRowStyleEqual(left, right *CompactRowStyle) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func decodeCompactRow(text string, runs []CompactRowRun, cells []CompactRowCell) []Cell {
	if text != "" || (len(runs) == 0 && len(cells) == 0) {
		return decodeCompactRowText(text, CellStyle{})
	}
	if len(runs) > 0 {
		out := make([]Cell, 0, compactRowRunCellCount(runs))
		for _, run := range runs {
			out = append(out, decodeCompactRowTextWithLink(run.Text, compactRowCellStyle(run.Style), run.LinkURL, run.LinkParams)...)
		}
		return out
	}
	out := make([]Cell, len(cells))
	for i, cell := range cells {
		out[i] = Cell{Content: cell.Content, Width: cell.Width, Style: compactRowCellStyle(cell.Style), LinkURL: cell.LinkURL, LinkParams: cell.LinkParams}
	}
	return out
}

func compactRowRunCellCount(runs []CompactRowRun) int {
	total := 0
	for _, run := range runs {
		total += utf8.RuneCountInString(run.Text)
	}
	return total
}

func decodeCompactRowText(text string, style CellStyle) []Cell {
	return decodeCompactRowTextWithLink(text, style, "", "")
}

func decodeCompactRowTextWithLink(text string, style CellStyle, linkURL string, linkParams string) []Cell {
	if text == "" {
		return nil
	}
	out := make([]Cell, 0, utf8.RuneCountInString(text))
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		if r < utf8.RuneSelf {
			out = append(out, Cell{Content: compactASCIIStrings[byte(r)], Width: 1, Style: style, LinkURL: linkURL, LinkParams: linkParams})
		} else {
			out = append(out, Cell{Content: text[:size], Width: 1, Style: style, LinkURL: linkURL, LinkParams: linkParams})
		}
		text = text[size:]
	}
	return out
}

func compactRowCellStyle(style *CompactRowStyle) CellStyle {
	if style == nil {
		return CellStyle{}
	}
	return CellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

type ChannelAllocator struct {
	mu       sync.Mutex
	next     uint16
	freeList []uint16
}

func NewChannelAllocator() *ChannelAllocator {
	return &ChannelAllocator{}
}

func (a *ChannelAllocator) Alloc() (uint16, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n := len(a.freeList); n > 0 {
		ch := a.freeList[n-1]
		a.freeList = a.freeList[:n-1]
		return ch, nil
	}
	if a.next == ^uint16(0) {
		return 0, errors.New("protocol: no channels available")
	}
	a.next++
	if a.next == 0 {
		a.next++
	}
	return a.next, nil
}

func (a *ChannelAllocator) Free(ch uint16) {
	if ch == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.freeList = append(a.freeList, ch)
}
