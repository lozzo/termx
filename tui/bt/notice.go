package bt

import "time"

type NoticeLevel string

const (
	NoticeLevelInfo  NoticeLevel = "info"
	NoticeLevelError NoticeLevel = "error"
)

type Notice struct {
	ID        string
	Level NoticeLevel
	Text      string
	CreatedAt time.Time
}
