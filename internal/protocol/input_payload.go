package protocol

import "fmt"

const inputParamsPayloadMagic = "TIP1"

func encodeInputParamsPayload(params InputParams) []byte {
	enc := binaryEncoder{buf: make([]byte, 0, len(inputParamsPayloadMagic)+len(params.TerminalID)+len(params.SurfaceID)+len(params.ViewID)+len(params.Data)+32)}
	enc.appendBytes([]byte(inputParamsPayloadMagic))
	enc.appendString(params.TerminalID)
	enc.appendUvarint(uint64(params.Channel))
	enc.appendString(params.SurfaceID)
	enc.appendString(params.ViewID)
	enc.appendUvarint(uint64(len(params.Data)))
	enc.appendBytes(params.Data)
	return enc.buf
}

func decodeInputParamsPayload(payload []byte) (InputParams, error) {
	dec := binaryDecoder{data: payload}
	if !dec.consumeMagic(inputParamsPayloadMagic) {
		return InputParams{}, fmt.Errorf("invalid input params payload magic")
	}
	terminalID, err := dec.readString()
	if err != nil {
		return InputParams{}, err
	}
	channel, err := dec.readUvarint()
	if err != nil {
		return InputParams{}, err
	}
	if channel > uint64(^uint16(0)) {
		return InputParams{}, fmt.Errorf("input channel out of range: %d", channel)
	}
	surfaceID, err := dec.readString()
	if err != nil {
		return InputParams{}, err
	}
	viewID, err := dec.readString()
	if err != nil {
		return InputParams{}, err
	}
	dataLen, err := dec.readUvarint()
	if err != nil {
		return InputParams{}, err
	}
	if uint64(len(dec.data)-dec.off) < dataLen {
		return InputParams{}, fmt.Errorf("unexpected EOF")
	}
	data := append([]byte(nil), dec.data[dec.off:dec.off+int(dataLen)]...)
	dec.off += int(dataLen)
	if dec.off != len(dec.data) {
		return InputParams{}, fmt.Errorf("trailing input params payload data")
	}
	return InputParams{
		TerminalID: terminalID,
		Channel:    uint16(channel),
		SurfaceID:  surfaceID,
		ViewID:     viewID,
		Data:       data,
	}, nil
}
