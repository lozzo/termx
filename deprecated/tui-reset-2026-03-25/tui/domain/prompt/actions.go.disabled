package prompt

type ActionID string

const (
	ActionSubmit ActionID = "submit"
	ActionCancel ActionID = "cancel"
)

type ActionRow struct {
	ID    ActionID
	Label string
}

// ActionRows 统一 prompt 动作行定义，避免 renderer 和鼠标命中逻辑各自维护一份。
func ActionRows() []ActionRow {
	return []ActionRow{
		{ID: ActionSubmit, Label: "submit"},
		{ID: ActionCancel, Label: "cancel"},
	}
}
