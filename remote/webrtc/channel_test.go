package webrtc

import (
	"errors"
	"testing"

	pionwebrtc "github.com/pion/webrtc/v4"
)

func TestChannelSendFailureNotifiesLifecycleOnce(t *testing.T) {
	sendErr := errors.New("data channel is closed")
	dataChannel := &failingDataChannel{sendErr: sendErr}
	channel := newChannel(dataChannel)
	closed := 0
	channel.SetCloseHandler(func() { closed++ })

	if err := channel.Send([]byte("request")); !errors.Is(err, sendErr) {
		t.Fatalf("send error = %v, want %v", err, sendErr)
	}
	if err := channel.Send([]byte("retry")); !errors.Is(err, sendErr) {
		t.Fatalf("second send error = %v, want %v", err, sendErr)
	}
	if closed != 1 {
		t.Fatalf("close notifications = %d, want 1", closed)
	}
}

type failingDataChannel struct {
	sendErr error
}

func (*failingDataChannel) OnClose(func())                                {}
func (*failingDataChannel) OnMessage(func(pionwebrtc.DataChannelMessage)) {}
func (*failingDataChannel) OnBufferedAmountLow(func())                    {}
func (*failingDataChannel) BufferedAmount() uint64                        { return 0 }
func (*failingDataChannel) SetBufferedAmountLowThreshold(uint64)          {}
func (channel *failingDataChannel) Send([]byte) error                     { return channel.sendErr }
func (*failingDataChannel) Close() error                                  { return nil }
