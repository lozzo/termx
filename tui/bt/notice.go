package bt

type NoticeLevel string

const (
	NoticeLevelInfo  NoticeLevel = "info"
	NoticeLevelError NoticeLevel = "error"
)

type Notice struct {
	Level NoticeLevel
	Text  string
}
