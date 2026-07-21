package edge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/relaycontrol"
	cloudrelay "github.com/muxvia/muxvia/private/cloud/relay"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

type relayControlState struct {
	Attached   bool
	Generation uint64
	LastError  string
}

type relayCommandReplay struct {
	digest    [sha256.Size]byte
	result    *cloudpb.RelayCommandResult
	expiresAt time.Time
}

const relayCommandSettlementTimeout = 15 * time.Second

// relayControlClient 是 Edge 内 Relay identity 的独立 transport owner。
// 它只通过 Relay Server port 关闭 allocation，不读取或修改 Hub state。
type relayControlClient struct {
	controllerURL string
	metadata      *cloudpb.EdgeDeploymentMetadata
	privateKey    ed25519.PrivateKey
	relay         *cloudrelay.Server
	outbox        *cloudrelay.UsageOutbox
	usageMu       *sync.Mutex
	reportUsage   func(context.Context) ([]*cloudpb.RelayUsageAck, error)
	httpClient    *http.Client
	mu            sync.Mutex
	state         relayControlState
	replay        map[string]relayCommandReplay
	sequence      uint64
}

func newRelayControlClient(controllerURL string, metadata *cloudpb.EdgeDeploymentMetadata, privateKey ed25519.PrivateKey, relay *cloudrelay.Server, outbox *cloudrelay.UsageOutbox, usageMu *sync.Mutex, reportUsage func(context.Context) ([]*cloudpb.RelayUsageAck, error)) (*relayControlClient, error) {
	if controllerURL == "" || metadata == nil || metadata.GetRelayId() == "" || len(privateKey) != ed25519.PrivateKeySize || relay == nil || outbox == nil || usageMu == nil || reportUsage == nil {
		return nil, fmt.Errorf("Relay control client dependencies are required")
	}
	return &relayControlClient{controllerURL: controllerURL, metadata: proto.Clone(metadata).(*cloudpb.EdgeDeploymentMetadata), privateKey: append(ed25519.PrivateKey(nil), privateKey...), relay: relay, outbox: outbox, usageMu: usageMu, reportUsage: reportUsage, httpClient: http.DefaultClient, replay: make(map[string]relayCommandReplay)}, nil
}

func (client *relayControlClient) State() relayControlState {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.state
}

func (client *relayControlClient) Run(ctx context.Context) error {
	for {
		if err := client.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			client.mu.Lock()
			client.state.Attached = false
			client.state.LastError = err.Error()
			client.mu.Unlock()
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (client *relayControlClient) runOnce(ctx context.Context) error {
	challengeResponse := &cloudpb.RelayControlChallengeResponse{}
	if err := client.unary(ctx, relaycontrol.ChallengePath, &cloudpb.RelayControlChallengeRequest{RelayId: client.metadata.GetRelayId(), EdgeDeploymentId: client.metadata.GetEdgeDeploymentId(), RelayControlIdentityFingerprint: client.metadata.GetRelayControlIdentityFingerprint()}, challengeResponse); err != nil {
		return err
	}
	proof, err := relaycontrol.ChallengeProofBytes(challengeResponse.GetChallenge(), client.metadata)
	if err != nil {
		return err
	}
	hello := &cloudpb.RelayHello{Deployment: proto.Clone(client.metadata).(*cloudpb.EdgeDeploymentMetadata), ChallengeId: challengeResponse.GetChallenge().GetChallengeId(), ChallengeSignature: ed25519.Sign(client.privateKey, proof), SoftwareVersion: "development"}
	body, err := proto.Marshal(hello)
	if err != nil {
		return err
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, client.controllerURL+relaycontrol.OpenPath, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/x-protobuf")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/x-muxvia-cloud-stream" {
		return fmt.Errorf("Relay control open failed with status %d", response.StatusCode)
	}
	var generation, controllerSequence uint64
	for {
		envelope := &cloudpb.RelayControlEnvelope{}
		if err := readRelayControlFrame(response.Body, envelope); err != nil {
			return err
		}
		now := time.Now().UTC()
		if envelope.GetRelayId() != client.metadata.GetRelayId() || envelope.GetEdgeDeploymentId() != client.metadata.GetEdgeDeploymentId() || envelope.GetSenderRole() != cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_CONTROLLER || envelope.GetIssuedAtUnixMillis() > now.UnixMilli() || envelope.GetExpiresAtUnixMillis() <= now.UnixMilli() {
			return errors.New("Relay control envelope rejected")
		}
		if generation == 0 {
			ready := envelope.GetReady()
			if ready == nil || ready.GetRelayControlGeneration() == 0 || ready.GetRelayControlGeneration() != envelope.GetRelayControlGeneration() || envelope.GetSenderSequence() != 1 {
				return errors.New("Relay control generation rejected")
			}
			generation, controllerSequence = ready.GetRelayControlGeneration(), 1
			client.mu.Lock()
			client.state = relayControlState{Attached: true, Generation: generation}
			client.sequence = 0
			client.mu.Unlock()
			continue
		}
		if envelope.GetRelayControlGeneration() != generation || envelope.GetSenderSequence() != controllerSequence+1 {
			return errors.New("Relay control sequence rejected")
		}
		controllerSequence = envelope.GetSenderSequence()
		command := envelope.GetCommand()
		if command == nil || command.GetRelayControlGeneration() != envelope.GetRelayControlGeneration() {
			return errors.New("Relay control command rejected")
		}
		result := client.execute(ctx, command, time.Now().UTC())
		if err := client.reportResult(ctx, result); err != nil {
			return err
		}
	}
}

func (client *relayControlClient) execute(ctx context.Context, command *cloudpb.RelayControlCommand, now time.Time) *cloudpb.RelayCommandResult {
	result := &cloudpb.RelayCommandResult{CommandId: command.GetCommandId(), RelayId: client.metadata.GetRelayId(), RelayControlGeneration: command.GetRelayControlGeneration(), ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_REJECTED, CompletedAtUnixMillis: now.UnixMilli()}
	stableCommand := proto.Clone(command).(*cloudpb.RelayControlCommand)
	stableCommand.RelayControlGeneration = 0
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(stableCommand)
	if err != nil || command.GetCommandId() == "" || command.GetTarget() == nil || command.GetTarget().GetRelayId() != client.metadata.GetRelayId() || command.GetExpiresAtUnixMillis() <= now.UnixMilli() || command.GetIssuedAtUnixMillis() > now.UnixMilli() {
		result.ErrorCode = "invalid_relay_command"
		return result
	}
	digest := sha256.Sum256(payload)
	client.mu.Lock()
	for commandID, replay := range client.replay {
		if !now.Before(replay.expiresAt) {
			delete(client.replay, commandID)
		}
	}
	if replay, ok := client.replay[command.GetCommandId()]; ok {
		client.mu.Unlock()
		if replay.digest != digest {
			result.ErrorCode = "command_replay_conflict"
			return result
		}
		replayed := proto.Clone(replay.result).(*cloudpb.RelayCommandResult)
		replayed.RelayControlGeneration = command.GetRelayControlGeneration()
		return replayed
	}
	client.mu.Unlock()
	target := command.GetTarget()
	validKind := command.GetCommandKind() == cloudpb.RelayControlCommandKind_RELAY_CONTROL_COMMAND_KIND_CLOSE_LEASE_ALLOCATIONS && target.GetLeaseId() != "" && target.GetManagedSessionId() == "" || command.GetCommandKind() == cloudpb.RelayControlCommandKind_RELAY_CONTROL_COMMAND_KIND_CLOSE_SESSION_ALLOCATIONS && target.GetManagedSessionId() != "" && target.GetLeaseId() == ""
	if !validKind {
		result.ErrorCode = "invalid_relay_target"
		return result
	}
	result.LeaseId = target.GetLeaseId()
	result.Allocations = client.relay.CloseAllocations(target.GetLeaseId(), target.GetManagedSessionId())
	partial := false
	for _, allocation := range result.GetAllocations() {
		if allocation.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED {
			partial = true
		}
	}
	client.usageMu.Lock()
	if err := client.relay.FlushUsageOutboxFor(client.outbox, "remote_revoke", target.GetLeaseId(), target.GetManagedSessionId()); err != nil {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_PARTIAL
		result.ErrorCode = "usage_drain_failed"
		result.CompletedAtUnixMillis = time.Now().UTC().UnixMilli()
	} else {
		result.UsageDrainComplete = true
		settlementDeadline := now.Add(relayCommandSettlementTimeout)
		if commandDeadline := time.UnixMilli(command.GetExpiresAtUnixMillis()); commandDeadline.Before(settlementDeadline) {
			settlementDeadline = commandDeadline
		}
		acks, settled := client.settleTargetUsage(ctx, settlementDeadline.UnixMilli(), target)
		result.SettledUsage = acks
		result.UsageSettlementComplete = settled
		if target.GetLeaseId() != "" {
			result.FinalUsageSequence = client.relay.FinalUsageSequence(target.GetLeaseId())
		}
		switch {
		case !settled || partial:
			result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_PARTIAL
			if !settled {
				result.ErrorCode = "usage_settlement_incomplete"
			}
		case len(result.GetAllocations()) == 0:
			result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_ALREADY_SATISFIED
		default:
			result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED
		}
		result.CompletedAtUnixMillis = time.Now().UTC().UnixMilli()
	}
	client.usageMu.Unlock()
	client.mu.Lock()
	client.replay[command.GetCommandId()] = relayCommandReplay{digest: digest, result: proto.Clone(result).(*cloudpb.RelayCommandResult), expiresAt: time.UnixMilli(command.GetExpiresAtUnixMillis())}
	client.mu.Unlock()
	return result
}

func (client *relayControlClient) settleTargetUsage(ctx context.Context, expiresAtMillis int64, target *cloudpb.RelayControlTarget) ([]*cloudpb.RelayUsageAck, bool) {
	var settled []*cloudpb.RelayUsageAck
	for time.Now().UnixMilli() < expiresAtMillis {
		before, beforeErr := client.outbox.Pending()
		targetEvents := make(map[string]bool)
		if beforeErr == nil {
			for _, record := range before {
				if targetMatchesUsage(target, record) {
					targetEvents[record.Event.EventID] = true
				}
			}
		}
		acks, err := client.reportUsage(ctx)
		if err == nil {
			for _, ack := range acks {
				if targetEvents[ack.GetEventId()] {
					settled = append(settled, ack)
				}
			}
		}
		pending, pendingErr := client.outbox.Pending()
		if pendingErr == nil {
			waiting := false
			for _, record := range pending {
				if targetMatchesUsage(target, record) {
					waiting = true
					break
				}
			}
			if !waiting {
				return settled, true
			}
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return settled, false
		case <-timer.C:
		}
	}
	return settled, false
}

func targetMatchesUsage(target *cloudpb.RelayControlTarget, record cloudrelay.UsageRecord) bool {
	return record.Event.TerminationReason != "" && (target.GetLeaseId() != "" && record.Event.LeaseID == target.GetLeaseId() || target.GetManagedSessionId() != "" && record.Event.ManagedSessionID == target.GetManagedSessionId())
}

func (client *relayControlClient) reportResult(ctx context.Context, result *cloudpb.RelayCommandResult) error {
	client.mu.Lock()
	client.sequence++
	sequence := client.sequence
	generation := client.state.Generation
	client.mu.Unlock()
	now := time.Now().UTC()
	envelope := &cloudpb.RelayRuntimeEnvelope{RelayId: client.metadata.GetRelayId(), EdgeDeploymentId: client.metadata.GetEdgeDeploymentId(), RelayControlGeneration: generation, SenderRole: cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_RELAY, SenderSequence: sequence, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Payload: &cloudpb.RelayRuntimeEnvelope_CommandResult{CommandResult: result}}
	response := &cloudpb.ReportRelayRuntimeResponse{}
	if err := client.unary(ctx, relaycontrol.ReportPath, &cloudpb.ReportRelayRuntimeRequest{Envelopes: []*cloudpb.RelayRuntimeEnvelope{envelope}}, response); err != nil {
		return err
	}
	if response.GetAcceptedSenderSequence() != sequence {
		return errors.New("Relay runtime acknowledgement rejected")
	}
	return nil
}

func (client *relayControlClient) unary(ctx context.Context, path string, input, output proto.Message) error {
	body, err := proto.Marshal(input)
	if err != nil {
		return err
	}
	requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(requestContext, http.MethodPost, client.controllerURL+path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/x-protobuf")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/x-protobuf" {
		return fmt.Errorf("Relay control unary %s failed with status %d", path, response.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(responseBody) == 0 || len(responseBody) > 4<<20 {
		return errors.New("Relay control response is invalid")
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(responseBody, output); err != nil || len(output.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("Relay control response is invalid")
	}
	return nil
}

func readRelayControlFrame(reader io.Reader, target proto.Message) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > 4<<20 {
		return errors.New("Relay control frame size is invalid")
	}
	body := make([]byte, size)
	defer clear(body)
	if _, err := io.ReadFull(reader, body); err != nil {
		return err
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, target); err != nil || len(target.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("Relay control frame is invalid")
	}
	return nil
}
