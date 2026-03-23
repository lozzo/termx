package bt

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/domain/types"
)

type EffectHandler interface {
	Handle(effects []reducer.Effect) tea.Cmd
}

type Renderer interface {
	Render(state types.AppState) string
}

type ModelConfig struct {
	InitialState  types.AppState
	Mapper        IntentMapper
	Reducer       reducer.StateReducer
	EffectHandler EffectHandler
	Renderer      Renderer
}

type Model struct {
	state   types.AppState
	mapper  IntentMapper
	reducer reducer.StateReducer
	effects EffectHandler
	view    Renderer
}

type NoopEffectHandler struct{}

func (NoopEffectHandler) Handle(_ []reducer.Effect) tea.Cmd {
	return nil
}

type StaticRenderer struct{}

func (StaticRenderer) Render(_ types.AppState) string {
	return ""
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
	return &Model{
		state:   cfg.InitialState,
		mapper:  mapper,
		reducer: rd,
		effects: effects,
		view:    view,
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

// Update 是最小 bubbletea 壳的统一入口。
// 当前只把键盘事件归一化为 intent，再交给 reducer 和 effect handler，避免输入层直接改状态。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	var cmd tea.Cmd
	for _, in := range m.mapper.MapKey(m.state, key) {
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
