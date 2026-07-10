// Package datachannel 提供面向可靠有序 DataChannel 的 termx protocol transport。
package datachannel

import (
	"context"
	"io"
	"sync"
	"time"
)

const (
	defaultReceiveQueueCapacity = 256
	defaultSendBufferHigh       = 512 * 1024
	defaultSendBufferLow        = 128 * 1024
	defaultDrainTimeout         = 30 * time.Second
)

// Channel 是 WebRTC 实现需要适配的可靠有序消息通道。
// 该接口只暴露 protocol frame 传输和 buffered amount 背压，不承担信令、身份验证、grant 校验或 terminal lifecycle。
type Channel interface {
	SetMessageHandler(func([]byte))
	SetCloseHandler(func())
	BufferedAmount() uint64
	SetBufferedAmountLowThreshold(uint64)
	SetBufferedAmountLowHandler(func())
	Send([]byte) error
	Close() error
}

// Transport 把一个已经协商的可靠有序 DataChannel 投影为 message transport。
// 同一实例可以先承载 remote auth envelope，再在 CapabilityAccepted 后承载 termx frame；调用方在授权成功前不得交给 protocol client 或 core-v2。
type Transport struct {
	channel Channel
	recvCh  chan []byte
	drainCh chan struct{}
	done    chan struct{}
	sendMu  sync.Mutex
	close   sync.Once
}

// New 创建 DataChannel message transport。
// channel 必须保证消息可靠且有序；该构造函数不判断 auth/protocol 阶段、不创建 peer connection，也不提供旧 remote/session-token fallback。
func New(channel Channel) *Transport {
	transport := &Transport{
		channel: channel,
		recvCh:  make(chan []byte, defaultReceiveQueueCapacity),
		drainCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	if channel == nil {
		transport.closeDone()
		return transport
	}
	channel.SetMessageHandler(func(payload []byte) {
		frame := append([]byte(nil), payload...)
		select {
		case <-transport.done:
		case transport.recvCh <- frame:
		}
	})
	channel.SetCloseHandler(transport.closeDone)
	channel.SetBufferedAmountLowThreshold(defaultSendBufferLow)
	channel.SetBufferedAmountLowHandler(func() {
		select {
		case transport.drainCh <- struct{}{}:
		default:
		}
	})
	return transport
}

// Send 发送一个完整 termx protocol frame。
// 当 DataChannel 缓冲超过高水位时等待低水位通知；超时或关闭会失败，不允许丢帧或切换到其他 transport。
func (transport *Transport) Send(frame []byte) error {
	if transport == nil || transport.channel == nil {
		return io.EOF
	}
	for transport.channel.BufferedAmount() > defaultSendBufferHigh {
		timer := time.NewTimer(defaultDrainTimeout)
		select {
		case <-transport.drainCh:
			if !timer.Stop() {
				<-timer.C
			}
		case <-transport.done:
			if !timer.Stop() {
				<-timer.C
			}
			return io.EOF
		case <-timer.C:
			return context.DeadlineExceeded
		}
	}
	transport.sendMu.Lock()
	defer transport.sendMu.Unlock()
	select {
	case <-transport.done:
		return io.EOF
	default:
	}
	return transport.channel.Send(append([]byte(nil), frame...))
}

// Recv 接收一个完整 termx protocol frame。
// DataChannel 关闭时返回 io.EOF；接收内容是独立副本，不能被底层实现后续复用或修改。
func (transport *Transport) Recv() ([]byte, error) {
	if transport == nil {
		return nil, io.EOF
	}
	select {
	case <-transport.done:
		return nil, io.EOF
	default:
	}
	select {
	case <-transport.done:
		return nil, io.EOF
	case frame := <-transport.recvCh:
		return frame, nil
	}
}

// Close 关闭 DataChannel 并解除所有等待中的 Send/Recv。
// 关闭只结束当前 transport，不修改 endpoint registry、reducer state 或 daemon terminal lifecycle。
func (transport *Transport) Close() error {
	if transport == nil {
		return nil
	}
	transport.closeDone()
	transport.sendMu.Lock()
	defer transport.sendMu.Unlock()
	if transport.channel == nil {
		return nil
	}
	return transport.channel.Close()
}

// Done 返回当前 DataChannel transport 的生命周期结束信号。
func (transport *Transport) Done() <-chan struct{} {
	if transport == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return transport.done
}

func (transport *Transport) closeDone() {
	transport.close.Do(func() {
		close(transport.done)
	})
}
