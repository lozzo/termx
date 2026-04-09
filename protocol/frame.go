package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	Version      = 1
	MaxFrameSize = 4 << 20

	TypeHello    uint8 = 0x00
	TypeRequest  uint8 = 0x01
	TypeResponse uint8 = 0x02
	TypeEvent    uint8 = 0x03
	TypeError    uint8 = 0x04

	TypeOutput   uint8 = 0x10
	TypeInput    uint8 = 0x11
	TypeResize   uint8 = 0x12
	TypeBootstrapDone uint8 = 0x13
	TypeSyncLost uint8 = 0x16
	TypeClosed   uint8 = 0x17
)

var (
	ErrFrameTooLarge = errors.New("protocol: frame too large")
	ErrShortPayload  = errors.New("protocol: short payload")
)

type Encoder struct {
	w io.Writer
}

type Decoder struct {
	r io.Reader
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

func (e *Encoder) WriteFrame(channel uint16, typ uint8, payload []byte) error {
	frame, err := EncodeFrame(channel, typ, payload)
	if err != nil {
		return err
	}
	_, err = e.w.Write(frame)
	return err
}

func (d *Decoder) ReadFrame() (uint16, uint8, []byte, error) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(d.r, header); err != nil {
		return 0, 0, nil, err
	}
	channel := binary.BigEndian.Uint16(header[:2])
	typ := header[2]
	length := binary.BigEndian.Uint32(header[3:])
	if length > MaxFrameSize {
		return 0, 0, nil, ErrFrameTooLarge
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(d.r, payload); err != nil {
		return 0, 0, nil, err
	}
	return channel, typ, payload, nil
}

func EncodeFrame(channel uint16, typ uint8, payload []byte) ([]byte, error) {
	if len(payload) > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	frame := make([]byte, 7+len(payload))
	binary.BigEndian.PutUint16(frame[:2], channel)
	frame[2] = typ
	binary.BigEndian.PutUint32(frame[3:7], uint32(len(payload)))
	copy(frame[7:], payload)
	return frame, nil
}

func DecodeFrame(frame []byte) (uint16, uint8, []byte, error) {
	if len(frame) < 7 {
		return 0, 0, nil, ErrShortPayload
	}
	channel := binary.BigEndian.Uint16(frame[:2])
	typ := frame[2]
	length := binary.BigEndian.Uint32(frame[3:7])
	if length > MaxFrameSize {
		return 0, 0, nil, ErrFrameTooLarge
	}
	if int(length) != len(frame[7:]) {
		return 0, 0, nil, fmt.Errorf("protocol: malformed frame length")
	}
	// The payload aliases frame storage so hot paths like transport dispatch
	// can decode without paying for an extra copy per frame.
	return channel, typ, frame[7:], nil
}

func EncodeResizePayload(cols, rows uint16) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint16(payload[:2], cols)
	binary.BigEndian.PutUint16(payload[2:], rows)
	return payload
}

func DecodeResizePayload(payload []byte) (uint16, uint16, error) {
	if len(payload) != 4 {
		return 0, 0, ErrShortPayload
	}
	return binary.BigEndian.Uint16(payload[:2]), binary.BigEndian.Uint16(payload[2:]), nil
}

func EncodeSyncLostPayload(dropped uint64) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(dropped))
	return payload
}

func DecodeSyncLostPayload(payload []byte) (uint64, error) {
	if len(payload) != 4 {
		return 0, ErrShortPayload
	}
	return uint64(binary.BigEndian.Uint32(payload)), nil
}

func EncodeClosedPayload(code int) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(int32(code)))
	return payload
}

func DecodeClosedPayload(payload []byte) (int, error) {
	if len(payload) != 4 {
		return 0, ErrShortPayload
	}
	return int(int32(binary.BigEndian.Uint32(payload))), nil
}
