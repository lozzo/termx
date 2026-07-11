// Package ipc 实现公开 termx 进程与本机 Cloud Companion 之间的 framed protobuf transport。
//
// 每条 OS connection 对应一个 cloudcompanion connection domain；首帧必须是 Hello，
// caller role、request、cancel 和 stream id 都不能跨 socket 或 Named Pipe 复用。
package ipc

import (
	"encoding/binary"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"
)

const maxFrameBytes = 4 << 20

func writeFrame(writer io.Writer, message proto.Message) error {
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode companion IPC frame: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxFrameBytes {
		return fmt.Errorf("companion IPC frame size %d is invalid", len(payload))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeAll(writer, header[:]); err != nil {
		return fmt.Errorf("write companion IPC frame header: %w", err)
	}
	if err := writeAll(writer, payload); err != nil {
		return fmt.Errorf("write companion IPC frame payload: %w", err)
	}
	return nil
}

func readFrame(reader io.Reader, message proto.Message) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > maxFrameBytes {
		return fmt.Errorf("companion IPC frame size %d is invalid", size)
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return fmt.Errorf("read companion IPC frame payload: %w", err)
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, message); err != nil {
		return fmt.Errorf("decode companion IPC frame: %w", err)
	}
	return nil
}

func writeAll(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}
