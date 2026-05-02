package machines

import "time"

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type Machine struct {
	ID               string     `json:"id"`
	OwnerUserID      string     `json:"owner_user_id,omitempty"`
	MachinePublicKey string     `json:"machine_public_key"`
	DisplayName      string     `json:"display_name"`
	Hostname         string     `json:"hostname"`
	Platform         string     `json:"platform"`
	LastSeenAt       *time.Time `json:"last_seen_at,omitempty"`
}

type BootstrapInput struct {
	MachineID         string
	MachinePublicKey  string
	DisplayName       string
	Hostname          string
	Platform          string
	MachinePrivateKey string
}

type BootstrapResult struct {
	Machine    Machine `json:"machine"`
	ClaimToken string  `json:"claim_token,omitempty"`
}

type ClaimInput struct {
	UserID     string
	MachineID  string
	ClaimToken string
}

type AppCertificate struct {
	ID                   string     `json:"id"`
	MachineID            string     `json:"machine_id"`
	AppDeviceID          string     `json:"app_device_id"`
	AppPublicKey         string     `json:"app_public_key"`
	AppDisplayName       string     `json:"app_display_name"`
	CertificatePayload   string     `json:"certificate_payload"`
	CertificateSignature string     `json:"certificate_signature"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	ExpiresAt            time.Time  `json:"expires_at"`
}

type RegisterAppCertificateInput struct {
	UserID               string
	MachineID            string
	AppPublicKey         string
	AppDisplayName       string
	AppPrivateKey        string
	CertificatePayload   string
	CertificateSignature string
	ExpiresAt            time.Time
}
