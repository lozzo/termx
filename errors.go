package termx

import "errors"

var (
	ErrNotFound       = errors.New("termx: terminal not found")
	ErrDuplicateID    = errors.New("termx: terminal ID already exists")
	ErrInvalidCommand = errors.New("termx: command is required")
	ErrTerminalExited = errors.New("termx: terminal has exited")
	ErrSpawnFailed    = errors.New("termx: failed to spawn process")
	ErrServerClosed   = errors.New("termx: server is closed")
)
