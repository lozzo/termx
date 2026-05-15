package termx

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-proto/wire"
)

const (
	coreDiagnosticsInterval          = time.Second
	coreDiagnosticsLargePayloadBytes = 128 * 1024
	coreDiagnosticsScreenUpdateBytes = 32 * 1024
	coreDiagnosticsSlowOperation     = 500 * time.Millisecond
)

type transportFrameStats struct {
	logger    *slog.Logger
	remote    string
	direction string
	lastLog   time.Time

	frames             int
	bytes              int
	controlFrames      int
	streamFrames       int
	screenUpdateFrames int
	inputFrames        int
	resizeFrames       int
	streamReadyFrames  int
	errorFrames        int
	maxPayloadBytes    int
	maxFrameBytes      int
	maxPayloadType     uint8
	maxPayloadChannel  uint16
}

func newTransportFrameStats(logger *slog.Logger, remote string, direction string) *transportFrameStats {
	return &transportFrameStats{
		logger:    logger,
		remote:    remote,
		direction: direction,
		lastLog:   time.Now(),
	}
}

func (s *transportFrameStats) record(channel uint16, typ uint8, payloadBytes int, frameBytes int) {
	if s == nil {
		return
	}
	s.frames++
	s.bytes += frameBytes
	if channel == 0 {
		s.controlFrames++
	} else {
		s.streamFrames++
	}
	switch typ {
	case wire.TypeScreenUpdate:
		s.screenUpdateFrames++
	case wire.TypeInput:
		s.inputFrames++
	case wire.TypeResize:
		s.resizeFrames++
	case wire.TypeStreamReady:
		s.streamReadyFrames++
	case wire.TypeError:
		s.errorFrames++
	}
	if payloadBytes > s.maxPayloadBytes {
		s.maxPayloadBytes = payloadBytes
		s.maxFrameBytes = frameBytes
		s.maxPayloadType = typ
		s.maxPayloadChannel = channel
	}
	if payloadBytes >= coreDiagnosticsLargePayloadBytes && s.logger != nil {
		s.logger.Warn(
			"termx transport large frame",
			"remote", s.remote,
			"direction", s.direction,
			"channel", channel,
			"type", protocolFrameTypeName(typ),
			"payload_bytes", payloadBytes,
			"frame_bytes", frameBytes,
		)
	}
	if time.Since(s.lastLog) >= coreDiagnosticsInterval {
		s.flush("termx transport frame stats")
	}
}

func (s *transportFrameStats) flush(message string) {
	if s == nil || s.logger == nil || s.frames == 0 {
		if s != nil {
			s.lastLog = time.Now()
		}
		return
	}
	s.logger.Info(
		message,
		"remote", s.remote,
		"direction", s.direction,
		"frames", s.frames,
		"bytes", s.bytes,
		"control_frames", s.controlFrames,
		"stream_frames", s.streamFrames,
		"screen_update_frames", s.screenUpdateFrames,
		"input_frames", s.inputFrames,
		"resize_frames", s.resizeFrames,
		"stream_ready_frames", s.streamReadyFrames,
		"error_frames", s.errorFrames,
		"max_payload_bytes", s.maxPayloadBytes,
		"max_frame_bytes", s.maxFrameBytes,
		"max_type", protocolFrameTypeName(s.maxPayloadType),
		"max_channel", s.maxPayloadChannel,
	)
	s.lastLog = time.Now()
	s.frames = 0
	s.bytes = 0
	s.controlFrames = 0
	s.streamFrames = 0
	s.screenUpdateFrames = 0
	s.inputFrames = 0
	s.resizeFrames = 0
	s.streamReadyFrames = 0
	s.errorFrames = 0
	s.maxPayloadBytes = 0
	s.maxFrameBytes = 0
	s.maxPayloadType = 0
	s.maxPayloadChannel = 0
}

func protocolFrameTypeName(typ uint8) string {
	switch typ {
	case wire.TypeHello:
		return "hello"
	case wire.TypeRequest:
		return "request"
	case wire.TypeResponse:
		return "response"
	case wire.TypeResponseBinary:
		return "response_binary"
	case wire.TypeEvent:
		return "event"
	case wire.TypeError:
		return "error"
	case wire.TypeInput:
		return "input"
	case wire.TypeResize:
		return "resize"
	case wire.TypeBootstrapDone:
		return "bootstrap_done"
	case wire.TypeScreenUpdate:
		return "screen_update"
	case wire.TypeStreamReady:
		return "stream_ready"
	case wire.TypeSyncLost:
		return "sync_lost"
	case wire.TypeClosed:
		return "closed"
	default:
		return "unknown"
	}
}

func streamMessageTypeName(typ StreamMessageType) string {
	switch typ {
	case StreamSyncLost:
		return "sync_lost"
	case StreamClosed:
		return "closed"
	case StreamResize:
		return "resize"
	case StreamBootstrapDone:
		return "bootstrap_done"
	case StreamScreenUpdate:
		return "screen_update"
	default:
		return "unknown"
	}
}

func diagnosticDurationMillis(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return d.Milliseconds()
}

func coreDebugEnvEnabled(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on", "debug":
		return true
	default:
		return false
	}
}

func coreScreenUpdateDebugEnabled() bool {
	return coreDebugEnvEnabled("TERMX_CORE_SCREEN_UPDATE_DEBUG")
}
