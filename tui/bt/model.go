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
	Render(state types.AppState) string
}

type UnmappedKeyHandler interface {
	HandleKey(state types.AppState, msg tea.KeyMsg) tea.Cmd
}

type NoticeScheduler interface {
	ScheduleTimeout(id string, after time.Duration) tea.Cmd
}

type ModelConfig struct {
	InitialState       types.AppState
	Mapper             IntentMapper
	Reducer            reducer.StateReducer
	EffectHandler      EffectHandler
	Renderer           Renderer
	UnmappedKeyHandler UnmappedKeyHandler
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
	noticeScheduler NoticeScheduler
	noticeTimeout   time.Duration
}

type NoopEffectHandler struct{}

func (NoopEffectHandler) Handle(_ []reducer.Effect) tea.Cmd {
	return nil
}

type StaticRenderer struct{}

func (StaticRenderer) Render(_ types.AppState) string {
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
		mapper:          mapper,
		reducer:         rd,
		effects:         effects,
		view:            view,
		unmappedKeys:    cfg.UnmappedKeyHandler,
		noticeScheduler: noticeScheduler,
		noticeTimeout:   noticeTimeout,
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

// Update 是最小 bubbletea 壳的统一入口。
// 当前只把键盘事件归一化为 intent，再交给 reducer 和 effect handler，避免输入层直接改状态。
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
	case FeedbackMsg:
		noticeCmd := m.appendNotices(msgValue.Notices)
		nextModel, intentCmd := m.applyIntents(msgValue.Intents)
		return nextModel, batchCmd(noticeCmd, intentCmd)
	case noticeTimeoutMsg:
		m.removeNotice(msgValue.ID)
		return m, nil
	default:
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
	return m.view.Render(m.state)
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
