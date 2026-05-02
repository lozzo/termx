package rendezvous

import "time"

const (
	MessageOffer     = "offer"
	MessageAnswer    = "answer"
	MessageCandidate = "candidate"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type ICEServer struct {
	URL string `json:"url"`
}

type Channel struct {
	ID         string      `json:"id"`
	Secret     string      `json:"secret"`
	Path       string      `json:"path"`
	MachineID  string      `json:"machine_id"`
	TerminalID string      `json:"terminal_id"`
	ExpiresAt  time.Time   `json:"expires_at"`
	ICEServers []ICEServer `json:"ice_servers"`
}

type Message struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateChannelInput struct {
	UserID     string
	MachineID  string
	TerminalID string
	TTL        time.Duration
}

type SendMessageInput struct {
	ChannelID string
	Secret    string
	Type      string
	Payload   string
}

type ListMessagesInput struct {
	ChannelID string
	Secret    string
}
