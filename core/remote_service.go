package core

import (
	"context"
	"errors"
	"time"
)

var ErrRemoteServiceUnavailable = errors.New("remote service is not configured")

// RemoteStatus 是 daemon remote runtime 的 core-native 状态投影。
type RemoteStatus struct {
	State, Detail, DeviceID, DeviceName, ControlURL, HubURL, DataDirectory, Mode string
	HubURLs                                                                      []string
	AllowLAN                                                                     bool
	TerminalCount                                                                int
	UpdatedAt                                                                    time.Time
}

// RemotePairStartRequest 是启动本地配对展示的 core-native 输入。
type RemotePairStartRequest struct {
	LocalPairURL            string
	TTLSeconds              int
	AuthorizationTTLSeconds int
}

// RemotePairStartResult 是一次配对会话的 core-native secret-bearing 结果。
type RemotePairStartResult struct {
	Type, MachineID, MachineName, LocalPairURL, PairSessionID, PairSecret, AnswerProofSecret string
	ExpiresAt                                                                                time.Time
}

// RemoteLocalEnableRequest 是 local web/ICE runtime 的 core-native装配输入。
type RemoteLocalEnableRequest struct {
	LocalWebAddress, ICETCPAddress, ControlURL, AccessToken, Region string
	HubURLs                                                         []string
}

// RemoteLocalStatus 是 local remote runtime 的 core-native 状态。
type RemoteLocalStatus struct {
	Enabled                                bool
	HTTPURL, LocalWebAddress, LocalPairURL string
	ICETCPEnabled                          bool
	ICETCPAddress                          string
	ICETCPPort                             int
	UpdatedAt                              time.Time
}

// RemoteService 是 core-v2 daemon 对 remote runtime 的 typed hook。
// 它只接受 core-native 类型，不承接 protocol/application DTO。
type RemoteService interface {
	Status(ctx context.Context) (RemoteStatus, error)
	PairStart(ctx context.Context, request RemotePairStartRequest) (RemotePairStartResult, error)
	LocalEnable(ctx context.Context, request RemoteLocalEnableRequest) (RemoteLocalStatus, error)
	LocalStatus(ctx context.Context) (RemoteLocalStatus, error)
	LocalDisable(ctx context.Context) (RemoteLocalStatus, error)
}
