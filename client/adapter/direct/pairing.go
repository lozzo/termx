package direct

import (
	"context"
	"fmt"
	"io"
	"time"

	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/shared/remoteauth"
)

// PairingConnector 通过 daemon embedded signaling 与 ICE-TCP 建立一次性 PairingExchange peer。
// 它不执行 capability auth 或 Hello；PairingService 成功或失败后会精确关闭当前 DataChannel/peer。
type PairingConnector struct {
	Peers     PeerFactory
	Signaling SignalingClient
	Random    io.Reader
	Now       func() time.Time
	Phase     func(clientruntime.EndpointPhase)
}

// Redeem 使用当前 Direct attempt 的 Endpoint pin 与实际 DTLS certificate 兑换 PairingTicket。
// ticket、ClientAccessIdentity 和 signer 只进入 DataChannel 内的公共 pairing 状态机，不进入 embedded signaling。
func (connector *PairingConnector) Redeem(ctx context.Context, request clientruntime.AttemptRequest, pairing remoteauth.ClientPairingRequest) (remoteauth.PairingExchangeResult, error) {
	if err := clientruntime.ValidatePairingAttempt(request, pairing); err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	if connector == nil {
		return remoteauth.PairingExchangeResult{}, fmt.Errorf("direct pairing connector is required")
	}
	opened, err := openDirectPeer(ctx, request, directPeerOptions{
		Peers: connector.Peers, Signaling: connector.Signaling, Random: connector.Random, Now: connector.Now, Phase: connector.Phase,
	})
	if err != nil {
		return remoteauth.PairingExchangeResult{}, err
	}
	return (clientruntime.PairingService{Now: connector.Now}).Redeem(ctx, request, opened, pairing)
}

var _ clientruntime.PairingPeerSession = (*openedDirectPeer)(nil)
