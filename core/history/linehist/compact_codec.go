package linehist

import (
	"encoding/binary"
	"errors"
)

func encodeCompactLines(lines []Line) []byte {
	payload := make([]byte, 0, compactLinesEncodedSize(lines))
	payload = binary.AppendUvarint(payload, uint64(len(lines)))
	for _, line := range lines {
		var flags byte
		if line.HardEnd {
			flags = lineFileFlagHardEnd
		}
		payload = append(payload, flags)
		payload = binary.AppendUvarint(payload, uint64(len(line.Runs)))
		for _, run := range line.Runs {
			payload = appendCompactString(payload, run.Text)
			payload = appendCompactString(payload, run.Style.FG)
			payload = appendCompactString(payload, run.Style.BG)
			payload = append(payload, lineFileStyleFlags(run.Style))
			payload = appendCompactString(payload, run.LinkURL)
			payload = appendCompactString(payload, run.LinkParams)
		}
	}
	return payload
}

func decodeCompactLines(payload []byte) ([]Line, error) {
	lineCount, size := binary.Uvarint(payload)
	if size <= 0 {
		return nil, errors.New("invalid compact history line count")
	}
	payload = payload[size:]
	if lineCount > uint64(len(payload)) {
		return nil, errors.New("compact history line count exceeds block")
	}
	lines := make([]Line, 0, int(lineCount))
	for i := uint64(0); i < lineCount; i++ {
		if len(payload) == 0 {
			return nil, errors.New("truncated compact history line")
		}
		line := Line{HardEnd: payload[0]&lineFileFlagHardEnd != 0}
		payload = payload[1:]
		runCount, size := binary.Uvarint(payload)
		if size <= 0 {
			return nil, errors.New("invalid compact history run count")
		}
		payload = payload[size:]
		if runCount > uint64(len(payload)) {
			return nil, errors.New("compact history run count exceeds block")
		}
		line.Runs = make([]Run, 0, int(runCount))
		for runIndex := uint64(0); runIndex < runCount; runIndex++ {
			var run Run
			var err error
			if run.Text, payload, err = takeCompactString(payload); err != nil {
				return nil, err
			}
			if run.Style.FG, payload, err = takeCompactString(payload); err != nil {
				return nil, err
			}
			if run.Style.BG, payload, err = takeCompactString(payload); err != nil {
				return nil, err
			}
			if len(payload) == 0 {
				return nil, errors.New("truncated compact history style")
			}
			applyLineFileStyleFlags(&run.Style, payload[0])
			payload = payload[1:]
			if run.LinkURL, payload, err = takeCompactString(payload); err != nil {
				return nil, err
			}
			if run.LinkParams, payload, err = takeCompactString(payload); err != nil {
				return nil, err
			}
			line.Runs = append(line.Runs, run)
		}
		lines = append(lines, line)
	}
	if len(payload) != 0 {
		return nil, errors.New("compact history block has trailing bytes")
	}
	return lines, nil
}

func appendCompactString(payload []byte, value string) []byte {
	payload = binary.AppendUvarint(payload, uint64(len(value)))
	return append(payload, value...)
}

func takeCompactString(payload []byte) (string, []byte, error) {
	length, size := binary.Uvarint(payload)
	if size <= 0 {
		return "", nil, errors.New("invalid compact history string length")
	}
	payload = payload[size:]
	if length > uint64(len(payload)) {
		return "", nil, errors.New("compact history string exceeds block")
	}
	return string(payload[:int(length)]), payload[int(length):], nil
}

func compactLinesEncodedSize(lines []Line) int {
	size := uvarintSize(uint64(len(lines)))
	for _, line := range lines {
		size += compactLineEncodedSize(line)
	}
	return size
}

func compactLineEncodedSize(line Line) int {
	size := 1 + uvarintSize(uint64(len(line.Runs)))
	for _, run := range line.Runs {
		for _, value := range []string{run.Text, run.Style.FG, run.Style.BG, run.LinkURL, run.LinkParams} {
			size += uvarintSize(uint64(len(value))) + len(value)
		}
		size++
	}
	return size
}

func uvarintSize(value uint64) int {
	size := 1
	for value >= 0x80 {
		value >>= 7
		size++
	}
	return size
}
