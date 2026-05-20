package main

import (
	"fmt"
	"time"

	"github.com/lozzow/termx/termx-proto/wirepb"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	"google.golang.org/protobuf/proto"
)

func timeToUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixNano()
}

func encodeRemoteResult(method string, value any) ([]byte, error) {
	switch method {
	case "remote.status":
		status, ok := value.(*remoteprotocol.Status)
		if !ok || status == nil {
			return nil, fmt.Errorf("remote.status result must be *remoteprotocol.Status")
		}
		return proto.Marshal(&wirepb.RemoteStatus{
			State:             string(status.State),
			Detail:            status.Detail,
			DeviceId:          status.DeviceID,
			DeviceName:        status.DeviceName,
			ControlUrl:        status.ControlURL,
			HubUrl:            status.HubURL,
			HubUrls:           append([]string(nil), status.HubURLs...),
			DataDir:           status.DataDir,
			Mode:              status.Mode,
			AllowLan:          status.AllowLAN,
			TerminalCount:     int32(status.TerminalCount),
			UpdatedAtUnixNano: timeToUnixNano(status.UpdatedAt),
		})
	case "remote.pair.start":
		result, ok := value.(*remoteprotocol.PairStartResult)
		if !ok || result == nil {
			return nil, fmt.Errorf("remote.pair.start result must be *remoteprotocol.PairStartResult")
		}
		return proto.Marshal(&wirepb.RemotePairStartResult{
			Type:              result.Type,
			MachineId:         result.MachineID,
			MachineName:       result.MachineName,
			LocalPairUrl:      result.LocalPairURL,
			PairSessionId:     result.PairSessionID,
			PairSecret:        result.PairSecret,
			AnswerProofSecret: result.AnswerProofSecret,
			ExpiresAtUnixNano: timeToUnixNano(result.ExpiresAt),
		})
	case "remote.local.enable", "remote.local.status", "remote.local.disable":
		status, ok := value.(*remoteprotocol.LocalStatus)
		if !ok || status == nil {
			return nil, fmt.Errorf("%s result must be *remoteprotocol.LocalStatus", method)
		}
		return proto.Marshal(&wirepb.RemoteLocalStatus{
			Enabled:           status.Enabled,
			HttpUrl:           status.HTTPURL,
			LocalWebAddr:      status.LocalWebAddr,
			LocalPairUrl:      status.LocalPairURL,
			IceTcpEnabled:     status.ICETCPEnabled,
			IceTcpAddr:        status.ICETCPAddr,
			IceTcpPort:        int32(status.ICETCPPort),
			UpdatedAtUnixNano: timeToUnixNano(status.UpdatedAt),
		})
	default:
		return nil, fmt.Errorf("unknown remote method: %s", method)
	}
}

func decodeRemotePairStartParams(payload []byte) (remoteprotocol.PairStartParams, error) {
	var msg wirepb.RemotePairStartParams
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return remoteprotocol.PairStartParams{}, err
	}
	return remoteprotocol.PairStartParams{
		LocalPairURL:   msg.GetLocalPairUrl(),
		TTLSeconds:     int(msg.GetTtlSeconds()),
		AuthTTLSeconds: int(msg.GetAuthTtlSeconds()),
	}, nil
}

func decodeRemoteLocalEnableParams(payload []byte) (remoteprotocol.LocalEnableParams, error) {
	var msg wirepb.RemoteLocalEnableParams
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return remoteprotocol.LocalEnableParams{}, err
	}
	return remoteprotocol.LocalEnableParams{
		LocalWebAddr: msg.GetLocalWebAddr(),
		ICETCPAddr:   msg.GetIceTcpAddr(),
		HubURLs:      append([]string(nil), msg.GetHubUrls()...),
		ControlURL:   msg.GetControlUrl(),
		AccessToken:  msg.GetAccessToken(),
		Region:       msg.GetRegion(),
	}, nil
}
