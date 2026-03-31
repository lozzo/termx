package modal

type PromptState struct {
	Kind       string
	Title      string
	Hint       string
	Value      string
	AllowEmpty bool
	Original   string
	PaneID     string
	Command    []string
	DefaultName string
	Name       string
	Tags       map[string]string
}
