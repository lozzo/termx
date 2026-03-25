package bt

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/domain/types"
)

const defaultNoticeTimeout = 5 * time.Second

type EffectHandler interface {
	Handle(effects []reducer.Effect) tea.Cmd
}

type Renderer interface {
	Render(state types.AppState, notices []Notice) string
}

type UnmappedKeyHandler interface {
	HandleKey(state types.AppState, msg tea.KeyMsg) tea.Cmd
}

type MessageHandler interface {
	HandleMessage(state types.AppState, msg tea.Msg) (bool, tea.Cmd)
}

type NoticeScheduler interface {
	ScheduleTimeout(id string, after time.Duration) tea.Cmd
}

type ModelConfig struct {
	InitialState       types.AppState
	InitCmd            tea.Cmd
	Mapper             IntentMapper
	Reducer            reducer.StateReducer
	EffectHandler      EffectHandler
	Renderer           Renderer
	UnmappedKeyHandler UnmappedKeyHandler
	MessageHandler     MessageHandler
	NoticeScheduler    NoticeScheduler
	NoticeTimeout      time.Duration
}

type Model struct {
	state           types.AppState
	notices         []Notice
	nextNoticeID    int
	mapper          IntentMapper
	reducer         reducer.StateReducer
	effects         EffectHandler
	view            Renderer
	unmappedKeys    UnmappedKeyHandler
	messages        MessageHandler
	noticeScheduler NoticeScheduler
	noticeTimeout   time.Duration
	initCmd         tea.Cmd
}

type NoopEffectHandler struct{}

func (NoopEffectHandler) Handle(_ []reducer.Effect) tea.Cmd {
	return nil
}

type StaticRenderer struct{}

func (StaticRenderer) Render(_ types.AppState, _ []Notice) string {
	return ""
}

type teaNoticeScheduler struct{}

type noticeTimeoutMsg struct {
	ID string
}

func (teaNoticeScheduler) ScheduleTimeout(id string, after time.Duration) tea.Cmd {
	if id == "" || after <= 0 {
		return nil
	}
	return tea.Tick(after, func(time.Time) tea.Msg {
		return noticeTimeoutMsg{ID: id}
	})
}

func NewModel(cfg ModelConfig) *Model {
	mapper := cfg.Mapper
	if mapper == nil {
		mapper = NewIntentMapper(Config{})
	}
	rd := cfg.Reducer
	if rd == nil {
		rd = reducer.New()
	}
	effects := cfg.EffectHandler
	if effects == nil {
		effects = NoopEffectHandler{}
	}
	view := cfg.Renderer
	if view == nil {
		view = StaticRenderer{}
	}
	noticeScheduler := cfg.NoticeScheduler
	if noticeScheduler == nil {
		noticeScheduler = teaNoticeScheduler{}
	}
	noticeTimeout := cfg.NoticeTimeout
	if noticeTimeout <= 0 {
		noticeTimeout = defaultNoticeTimeout
	}
	return &Model{
		state:           cfg.InitialState,
		initCmd:         cfg.InitCmd,
		mapper:          mapper,
		reducer:         rd,
		effects:         effects,
		view:            view,
		unmappedKeys:    cfg.UnmappedKeyHandler,
		messages:        cfg.MessageHandler,
		noticeScheduler: noticeScheduler,
		noticeTimeout:   noticeTimeout,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.initCmd
}

// Update 是最小 bubbletea 壳的统一入口。
// 当前把键盘和最小鼠标事件都归一化为 intent，再交给 reducer 和 effect handler，避免输入层直接改状态。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msgValue := msg.(type) {
	case tea.KeyMsg:
		intents := m.mapper.MapKey(m.state, msgValue)
		if len(intents) > 0 {
			return m.applyIntents(intents)
		}
		if m.unmappedKeys != nil {
			return m, m.unmappedKeys.HandleKey(m.state, msgValue)
		}
		return m, nil
	case tea.MouseMsg:
		intents := m.mapper.MapMouse(m.state, msgValue, m.View())
		if len(intents) > 0 {
			return m.applyIntents(intents)
		}
		return m, nil
	case FeedbackMsg:
		noticeCmd := m.appendNotices(msgValue.Notices)
		nextModel, intentCmd := m.applyIntents(msgValue.Intents)
		return nextModel, batchCmd(noticeCmd, intentCmd)
	case noticeTimeoutMsg:
		m.removeNotice(msgValue.ID)
		return m, nil
	default:
		if m.messages != nil {
			handled, cmd := m.messages.HandleMessage(m.state, msg)
			if handled {
				return m, cmd
			}
		}
		return m, nil
	}
}

func (m *Model) applyIntents(intents []intent.Intent) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, in := range intents {
		result := m.reducer.Reduce(m.state, in)
		m.state = result.State
		cmd = batchCmd(cmd, m.effects.Handle(result.Effects))
	}
	return m, cmd
}

func (m *Model) View() string {
	return m.view.Render(m.state, m.notices)
}

func (m *Model) State() types.AppState {
	return m.state
}

func (m *Model) Notices() []Notice {
	return append([]Notice(nil), m.notices...)
}

func (m *Model) appendNotices(notices []Notice) tea.Cmd {
	if len(notices) == 0 {
		return nil
	}
	var cmd tea.Cmd
	for _, notice := range notices {
		next := notice
		if next.Count <= 0 {
			next.Count = 1
		}
		if idx, ok := m.findNotice(next); ok {
			existing := m.notices[idx]
			next.ID = m.nextNoticeIDValue()
			if existing.Count > 0 {
				next.Count += existing.Count
			}
			if existing.CreatedAt.IsZero() {
				existing.CreatedAt = time.Now()
			}
			next.CreatedAt = existing.CreatedAt
			m.notices[idx] = next
			cmd = batchCmd(cmd, m.noticeScheduler.ScheduleTimeout(next.ID, m.noticeTimeout))
			continue
		}
		if next.ID == "" {
			next.ID = m.nextNoticeIDValue()
		}
		if next.CreatedAt.IsZero() {
			next.CreatedAt = time.Now()
		}
		m.notices = append(m.notices, next)
		cmd = batchCmd(cmd, m.noticeScheduler.ScheduleTimeout(next.ID, m.noticeTimeout))
	}
	return cmd
}

// findNotice 用 level+text 做最小 notice 聚合键，避免 runtime 重复错误刷屏。
func (m *Model) findNotice(target Notice) (int, bool) {
	for idx, notice := range m.notices {
		if notice.Level == target.Level && notice.Text == target.Text {
			return idx, true
		}
	}
	return 0, false
}

func (m *Model) removeNotice(id string) {
	if id == "" || len(m.notices) == 0 {
		return
	}
	filtered := m.notices[:0]
	for _, notice := range m.notices {
		if notice.ID == id {
			continue
		}
		filtered = append(filtered, notice)
	}
	m.notices = filtered
}

func (m *Model) nextNoticeIDValue() string {
	m.nextNoticeID++
	return fmt.Sprintf("notice-%d", m.nextNoticeID)
}

func batchCmd(current tea.Cmd, next tea.Cmd) tea.Cmd {
	switch {
	case current == nil:
		return next
	case next == nil:
		return current
	default:
		return tea.Batch(current, next)
	}
}
