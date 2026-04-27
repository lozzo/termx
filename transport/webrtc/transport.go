package webrtc

import (
	"fmt"

	"github.com/lozzow/termx/transport"
)

const (
	packetKindFrame byte = iota
	packetKindFragmentStart
	packetKindFragmentContinue
	packetKindFragmentEnd
)

// Keep chunk size under the more conservative DataChannel message sizes used by
// existing tgent transport code so termx frames can travel safely without
// changing the higher-level protocol framing.
const maxMessagePayloadBytes = 192 * 1024

type MessageChannel interface {
	Send([]byte) error
	Recv() ([]byte, error)
	Close() error
	Done() <-chan struct{}
}

type Transport struct {
	channel MessageChannel
}

var _ transport.Transport = (*Transport)(nil)

func NewTransport(channel MessageChannel) *Transport {
	return &Transport{channel: channel}
}

func (t *Transport) Send(frame []byte) error {
	if len(frame) <= maxMessagePayloadBytes {
		return t.channel.Send(append([]byte{packetKindFrame}, frame...))
	}
	for offset := 0; offset < len(frame); {
		end := offset + maxMessagePayloadBytes
		if end > len(frame) {
			end = len(frame)
		}
		kind := packetKindFragmentContinue
		switch {
		case offset == 0:
			kind = packetKindFragmentStart
		case end == len(frame):
			kind = packetKindFragmentEnd
		}
		chunk := make([]byte, 1+end-offset)
		chunk[0] = kind
		copy(chunk[1:], frame[offset:end])
		if err := t.channel.Send(chunk); err != nil {
			return err
		}
		offset = end
	}
	return nil
}

func (t *Transport) Recv() ([]byte, error) {
	msg, err := t.channel.Recv()
	if err != nil {
		return nil, err
	}
	if len(msg) == 0 {
		return nil, fmt.Errorf("transport/webrtc: empty message")
	}
	kind := msg[0]
	payload := msg[1:]
	switch kind {
	case packetKindFrame:
		return append([]byte(nil), payload...), nil
	case packetKindFragmentStart:
		buf := append([]byte(nil), payload...)
		for {
			next, err := t.channel.Recv()
			if err != nil {
				return nil, err
			}
			if len(next) == 0 {
				return nil, fmt.Errorf("transport/webrtc: empty fragment")
			}
			switch next[0] {
			case packetKindFragmentContinue:
				buf = append(buf, next[1:]...)
			case packetKindFragmentEnd:
				buf = append(buf, next[1:]...)
				return buf, nil
			default:
				return nil, fmt.Errorf("transport/webrtc: unexpected packet kind %d during fragmented frame", next[0])
			}
		}
	default:
		return nil, fmt.Errorf("transport/webrtc: unexpected packet kind %d", kind)
	}
}

func (t *Transport) Close() error {
	if t == nil || t.channel == nil {
		return nil
	}
	return t.channel.Close()
}

func (t *Transport) Done() <-chan struct{} {
	if t == nil || t.channel == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return t.channel.Done()
}
