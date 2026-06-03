package app

// Msg is the root message contract for the TUI-v3 runtime.
type Msg interface {
	isMsg()
}

// Effect is the root side-effect contract for the TUI-v3 runtime.
type Effect interface {
	isEffect()
}

// NoopMsg is a smoke-test message used before concrete app messages exist.
type NoopMsg struct{}

func (NoopMsg) isMsg() {}

// NoopEffect is a smoke-test effect used before concrete effects exist.
type NoopEffect struct{}

func (NoopEffect) isEffect() {}
