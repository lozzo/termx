package tui

import "github.com/lozzow/termx/protocol"

type Terminal struct {
	ID         string
	Name       string
	Command    []string
	Tags       map[string]string
	State      string
	ExitCode   *int
	Snapshot   *protocol.Snapshot
	Channel    uint16
	AttachMode string
}

func (t *Terminal) SetMetadata(name string, command []string, tags map[string]string) {
	if t == nil {
		return
	}
	t.Name = name
	t.Command = append([]string(nil), command...)
	if tags == nil {
		t.Tags = nil
		return
	}
	t.Tags = make(map[string]string, len(tags))
	for key, value := range tags {
		t.Tags[key] = value
	}
}
