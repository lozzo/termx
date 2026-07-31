package runtime

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/clientgateway"
	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/edge/reservation"
	"github.com/anytty/anytty/cloud/relayquota"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRequestRelayPersistsRequestedBeforeSendAndExposedBeforeReturn(t *testing.T) {
	now := time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC)
	runtime := newReservationRuntime(t, now)
	session := newFakeRelaySession()
	session.reserve = func(request *cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error) {
		record, found, err := runtime.journalRecord(request.GetReservationId())
		if err != nil || !found || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_REQUESTED || record.GetGrant() != nil {
			t.Fatalf("journal at send = %v found=%v err=%v", record, found, err)
		}
		grant := runtimeTestGrant(t, request.GetReservationId(), request.GetSessionId(), now, 0)
		return &cloudv1.RelayReserveResponse{ReservationId: request.GetReservationId(), RequestDigest: request.GetRequestDigest(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED, Grant: grant}, nil
	}
	runtime.setControlSession(session)
	runtime.controllerConnected.Store(true)
	material, err := runtime.RequestRelay(context.Background(), &clientgateway.RelayRequest{SessionID: "00000000-0000-4000-8000-000000000101", AccountID: "00000000-0000-4000-8000-000000000102", DaemonID: "00000000-0000-4000-8000-000000000103", ClientID: "client"})
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := runtime.journalRecord(material.GetReservationId())
	if err != nil || !found || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED {
		t.Fatalf("journal before credential return = %v found=%v err=%v", record, found, err)
	}
}

func TestReplayWaitsForInFlightReserveAndRefreshesDurableStage(t *testing.T) {
	now := time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC)
	runtime := newReservationRuntime(t, now)
	reserveStarted := make(chan *cloudv1.RelayReserveRequest, 1)
	finishReserve := make(chan struct{})
	session := newFakeRelaySession()
	session.reserve = func(request *cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error) {
		reserveStarted <- proto.Clone(request).(*cloudv1.RelayReserveRequest)
		<-finishReserve
		grant := runtimeTestGrant(t, request.GetReservationId(), request.GetSessionId(), now, 0)
		return &cloudv1.RelayReserveResponse{ReservationId: request.GetReservationId(), RequestDigest: request.GetRequestDigest(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED, Grant: grant}, nil
	}
	session.query = func(*cloudv1.RelayQueryRequest) (*cloudv1.RelayQueryResponse, error) {
		return nil, errors.New("replay used the stale REQUESTED stage")
	}
	session.settle = func(*cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
		return nil, errors.New("replay settled an in-flight reservation")
	}
	runtime.controlSession = session
	runtime.controllerConnected.Store(true)
	type requestResult struct {
		material *cloudv1.RelayICEConfig
		err      error
	}
	requestDone := make(chan requestResult, 1)
	go func() {
		material, err := runtime.RequestRelay(context.Background(), &clientgateway.RelayRequest{SessionID: "00000000-0000-4000-8000-000000000111", AccountID: "00000000-0000-4000-8000-000000000112", DaemonID: "00000000-0000-4000-8000-000000000113", ClientID: "client"})
		requestDone <- requestResult{material: material, err: err}
	}()
	request := <-reserveStarted
	record, found, err := runtime.journalRecord(request.GetReservationId())
	if err != nil || !found || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_REQUESTED {
		t.Fatalf("in-flight journal = %v found=%v err=%v", record, found, err)
	}
	replayAtLock := make(chan string, 1)
	allowReplayLock := make(chan struct{})
	runtime.beforeReplayLock = func(reservationID string) {
		replayAtLock <- reservationID
		<-allowReplayLock
	}
	replayDone := make(chan error, 1)
	go func() {
		replayDone <- runtime.replayRelayRecord(context.Background(), session, record)
	}()
	if replayID := <-replayAtLock; replayID != request.GetReservationId() {
		t.Fatalf("replay reservation=%s want=%s", replayID, request.GetReservationId())
	}
	close(finishReserve)
	result := <-requestDone
	if result.err != nil || result.material.GetReservationId() != request.GetReservationId() {
		t.Fatalf("reserve result material=%v err=%v", result.material, result.err)
	}
	close(allowReplayLock)
	if err := <-replayDone; err != nil {
		t.Fatal(err)
	}
	record, found, err = runtime.journalRecord(request.GetReservationId())
	if err != nil || !found || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED {
		t.Fatalf("journal after replay = %v found=%v err=%v", record, found, err)
	}
}

func TestRequestRelayOfflineCreatesNoDurableRequest(t *testing.T) {
	runtime := newReservationRuntime(t, time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC))
	if _, err := runtime.RequestRelay(context.Background(), &clientgateway.RelayRequest{SessionID: "session", AccountID: "account", DaemonID: "daemon", ClientID: "client"}); err == nil {
		t.Fatal("offline Relay reserve succeeded")
	}
	if depth, err := runtime.RelayJournalDepth(); err != nil || depth != 0 {
		t.Fatalf("offline request journal depth=%d err=%v", depth, err)
	}
}

func TestRenewResponseLossReplaysSameDurableSequence(t *testing.T) {
	now := time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC)
	runtime := newReservationRuntime(t, now)
	grant, material := seedExposedReservation(t, runtime, now)
	lost := newFakeRelaySession()
	close(lost.done)
	lost.renew = func(request *cloudv1.RelayRenewRequest) (*cloudv1.RelayRenewResponse, error) {
		record, _, err := runtime.journalRecord(request.GetReservationId())
		if err != nil || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING || record.GetPendingRenewSequence() != 1 {
			t.Fatalf("journal before renew send = %v err=%v", record, err)
		}
		return nil, errors.New("injected response loss")
	}
	runtime.setControlSession(lost)
	runtime.controllerConnected.Store(true)
	if _, err := runtime.RenewRelay(context.Background(), material); err == nil {
		t.Fatal("lost renew response succeeded")
	}

	replayed := newFakeRelaySession()
	replayed.renew = func(request *cloudv1.RelayRenewRequest) (*cloudv1.RelayRenewResponse, error) {
		if request.GetRenewSequence() != 1 || !bytes.Equal(request.GetPolicyDigest(), grant.GetPolicyDigest()) {
			t.Fatalf("renew replay = %v", request)
		}
		renewed := proto.Clone(grant).(*cloudv1.RelayGrant)
		renewed.RenewSequence = 1
		renewed.AuthorizedUntil = timestamppb.New(now.Add(2 * time.Minute))
		return &cloudv1.RelayRenewResponse{ReservationId: request.GetReservationId(), RenewSequence: 1, Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY, Grant: renewed}, nil
	}
	runtime.controlSession = replayed
	renewed, err := runtime.RenewRelay(context.Background(), material)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.GetExpiresAt().AsTime().Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("renewed credential = %v", renewed)
	}
	record, _, _ := runtime.journalRecord(grant.GetReservationId())
	if record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED || record.GetGrant().GetRenewSequence() != 1 {
		t.Fatalf("renewed journal = %v", record)
	}
}

func TestRelayShutdownFreezesExactAggregateBeforeAck(t *testing.T) {
	now := time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC)
	runtime := newReservationRuntime(t, now)
	grant, _ := seedExposedReservation(t, runtime, now)
	relay := &shutdownReservationRelay{}
	runtime.relayServer = relay
	session := newFakeRelaySession()
	session.settle = func(value *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
		if !relay.closed || value.GetKind() != cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT {
			t.Fatalf("settlement was sent before Relay became static: closed=%v value=%v", relay.closed, value)
		}
		record, found, err := runtime.journalRecord(value.GetReservationId())
		if err != nil || !found || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE {
			t.Fatalf("settlement was not durable before send: record=%v found=%v err=%v", record, found, err)
		}
		return settlementAck(value, grant, now.Add(time.Second)), nil
	}
	runtime.controlSession = session

	records, err := runtime.prepareRelayShutdown(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := runtime.journalRecord(grant.GetReservationId())
	if err != nil || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING {
		t.Fatalf("journal before drain = %v err=%v", record, err)
	}
	if err := relay.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.finishRelayShutdown(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	if _, found, err := runtime.journalRecord(grant.GetReservationId()); err != nil || found {
		t.Fatalf("ACK did not clear durable settlement: found=%v err=%v", found, err)
	}
	if live, err := runtime.state.RelayReservationLive(context.Background(), grant.GetReservationId()); err != nil || live {
		t.Fatalf("settled group remained live: live=%v err=%v", live, err)
	}
}

type shutdownReservationRelay struct{ closed bool }

func (*shutdownReservationRelay) Address() string { return "" }
func (*shutdownReservationRelay) Degraded() bool  { return false }
func (*shutdownReservationRelay) CloseSessionAllocations(context.Context, string) error {
	return nil
}
func (relay *shutdownReservationRelay) Close(context.Context) error {
	relay.closed = true
	return nil
}
func (relay *shutdownReservationRelay) StateCloseSafe() bool { return relay.closed }

func TestSettlementAckLossRetainsFactAndRestartExposedRecoversMax(t *testing.T) {
	now := time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC)
	runtime := newReservationRuntime(t, now)
	grant, _ := seedExposedReservation(t, runtime, now)
	settlement := exactZeroSettlement(grant, now.Add(time.Second))
	if err := runtime.journalMarkClosing(grant.GetReservationId()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.journalPutSettlement(settlement); err != nil {
		t.Fatal(err)
	}
	lost := newFakeRelaySession()
	lost.settle = func(*cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
		return nil, errors.New("injected ACK loss")
	}
	if err := runtime.deliverSettlement(context.Background(), lost, settlement); err == nil {
		t.Fatal("lost ACK deleted settlement")
	}
	if record, found, _ := runtime.journalRecord(grant.GetReservationId()); !found || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE {
		t.Fatalf("settlement after ACK loss = %v found=%v", record, found)
	}
	accepted := newFakeRelaySession()
	accepted.settle = func(value *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
		return settlementAck(value, grant, now.Add(2*time.Second)), nil
	}
	if err := runtime.deliverSettlement(context.Background(), accepted, settlement); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := runtime.journalRecord(grant.GetReservationId()); found {
		t.Fatal("committed ACK did not delete durable settlement")
	}

	restarted := newReservationRuntime(t, now)
	restartGrant, _ := seedJournalOnlyExposed(t, restarted, now)
	recoverySession := newFakeRelaySession()
	recoverySession.settle = func(value *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
		if value.GetKind() != cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX || value.GetIngressBytes() != 0 || value.GetEgressBytes() != 0 {
			t.Fatalf("restart settlement = %v", value)
		}
		return settlementAck(value, restartGrant, now.Add(3*time.Second)), nil
	}
	record, _, _ := restarted.journalRecord(restartGrant.GetReservationId())
	if err := restarted.replayRelayRecord(context.Background(), recoverySession, record); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := restarted.journalRecord(restartGrant.GetReservationId()); found {
		t.Fatal("recovery ACK did not delete exposed restart record")
	}
}

func TestRestartRequestedReplayNeverExposesAuthorityAndSettlesZero(t *testing.T) {
	now := time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC)
	runtime := newReservationRuntime(t, now)
	request := &cloudv1.RelayReserveRequest{
		ReservationId: "00000000-0000-4000-8000-000000000211",
		AccountId:     "00000000-0000-4000-8000-000000000212",
		DaemonId:      "00000000-0000-4000-8000-000000000213",
		ClientId:      "client",
		SessionId:     "00000000-0000-4000-8000-000000000214",
		ObservedAt:    timestamppb.New(now),
	}
	request.RequestDigest, _ = relayquota.ReserveRequestDigest(request)
	if err := runtime.journalCreateRequested(request); err != nil {
		t.Fatal(err)
	}
	grant := runtimeTestGrant(t, request.GetReservationId(), request.GetSessionId(), now, 0)
	session := newFakeRelaySession()
	session.query = func(query *cloudv1.RelayQueryRequest) (*cloudv1.RelayQueryResponse, error) {
		return &cloudv1.RelayQueryResponse{ReservationId: query.GetReservationId(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED}, nil
	}
	session.reserve = func(replayed *cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error) {
		if !proto.Equal(replayed, request) {
			t.Fatalf("REQUESTED replay changed payload: %v", replayed)
		}
		return &cloudv1.RelayReserveResponse{ReservationId: replayed.GetReservationId(), RequestDigest: replayed.GetRequestDigest(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED, Grant: grant}, nil
	}
	session.settle = func(settlement *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
		if settlement.GetKind() != cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT || settlement.GetIngressBytes() != 0 || settlement.GetEgressBytes() != 0 {
			t.Fatalf("REQUESTED restart settlement=%v", settlement)
		}
		record, found, err := runtime.journalRecord(request.GetReservationId())
		if err != nil || !found || record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE {
			t.Fatalf("zero settlement was not durable before send: record=%v found=%v err=%v", record, found, err)
		}
		return settlementAck(settlement, grant, now.Add(time.Second)), nil
	}
	record, _, _ := runtime.journalRecord(request.GetReservationId())
	if err := runtime.replayRelayRecord(context.Background(), session, record); err != nil {
		t.Fatal(err)
	}
	if _, found, err := runtime.journalRecord(request.GetReservationId()); err != nil || found {
		t.Fatalf("committed zero settlement remained durable: found=%v err=%v", found, err)
	}
	if live, err := runtime.state.RelayReservationLive(context.Background(), request.GetReservationId()); err != nil || live {
		t.Fatalf("REQUESTED restart restored Relay authority: live=%v err=%v", live, err)
	}
}

func TestReplayContinuesAfterDefinitiveRequestedRejection(t *testing.T) {
	now := time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC)
	runtime := newReservationRuntime(t, now)
	rejected := &cloudv1.RelayReserveRequest{
		ReservationId: "00000000-0000-4000-8000-000000000211",
		AccountId:     "00000000-0000-4000-8000-000000000212",
		DaemonId:      "00000000-0000-4000-8000-000000000213",
		ClientId:      "client-rejected",
		SessionId:     "00000000-0000-4000-8000-000000000214",
		ObservedAt:    timestamppb.New(now),
	}
	rejected.RequestDigest, _ = relayquota.ReserveRequestDigest(rejected)
	if err := runtime.journalCreateRequested(rejected); err != nil {
		t.Fatal(err)
	}

	settledRequest := proto.Clone(rejected).(*cloudv1.RelayReserveRequest)
	settledRequest.ReservationId = "00000000-0000-4000-8000-000000000221"
	settledRequest.SessionId = "00000000-0000-4000-8000-000000000224"
	settledRequest.ClientId = "client-settled"
	settledRequest.RequestDigest, _ = relayquota.ReserveRequestDigest(settledRequest)
	if err := runtime.journalCreateRequested(settledRequest); err != nil {
		t.Fatal(err)
	}
	grant := runtimeTestGrant(t, settledRequest.GetReservationId(), settledRequest.GetSessionId(), now, 0)
	if err := runtime.journalApplyGrant(grant); err != nil {
		t.Fatal(err)
	}
	settlement := exactZeroSettlement(grant, now.Add(time.Second))
	if err := runtime.journalPutSettlement(settlement); err != nil {
		t.Fatal(err)
	}

	settlements := 0
	session := newFakeRelaySession()
	session.query = func(query *cloudv1.RelayQueryRequest) (*cloudv1.RelayQueryResponse, error) {
		return &cloudv1.RelayQueryResponse{ReservationId: query.GetReservationId(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED}, nil
	}
	session.reserve = func(request *cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error) {
		return &cloudv1.RelayReserveResponse{ReservationId: request.GetReservationId(), RequestDigest: request.GetRequestDigest(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT, ErrorMessage: "definitive conflict"}, nil
	}
	session.settle = func(value *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
		settlements++
		return settlementAck(value, grant, now.Add(2*time.Second)), nil
	}
	runtime.replayRelayJournal(session)
	if settlements != 1 {
		t.Fatalf("later settlement deliveries=%d want=1", settlements)
	}
	for _, reservationID := range []string{rejected.GetReservationId(), settledRequest.GetReservationId()} {
		if _, found, err := runtime.journalRecord(reservationID); err != nil || found {
			t.Fatalf("reservation %s remained after replay: found=%v err=%v", reservationID, found, err)
		}
	}
}

type fakeRelaySession struct {
	done    chan struct{}
	reserve func(*cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error)
	renew   func(*cloudv1.RelayRenewRequest) (*cloudv1.RelayRenewResponse, error)
	settle  func(*cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error)
	query   func(*cloudv1.RelayQueryRequest) (*cloudv1.RelayQueryResponse, error)
}

func newFakeRelaySession() *fakeRelaySession            { return &fakeRelaySession{done: make(chan struct{})} }
func (session *fakeRelaySession) Done() <-chan struct{} { return session.done }
func (session *fakeRelaySession) ReserveRelay(_ context.Context, request *cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error) {
	return session.reserve(request)
}
func (session *fakeRelaySession) RenewRelay(_ context.Context, request *cloudv1.RelayRenewRequest) (*cloudv1.RelayRenewResponse, error) {
	return session.renew(request)
}
func (session *fakeRelaySession) SettleRelay(_ context.Context, value *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
	return session.settle(value)
}
func (session *fakeRelaySession) QueryRelay(_ context.Context, request *cloudv1.RelayQueryRequest) (*cloudv1.RelayQueryResponse, error) {
	return session.query(request)
}

func newReservationRuntime(t *testing.T, now time.Time) *Runtime {
	t.Helper()
	state := newRelayTestState(t, now)
	journal, err := reservation.Open(filepath.Join(t.TempDir(), "relay.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	deriver, err := policy.NewCredentialDeriver(bytes.Repeat([]byte{0x44}, 32), []string{"turn:edge.test:3478?transport=udp"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &Runtime{ctx: ctx, cancel: cancel, state: state, relayJournal: journal, credentialDeriver: deriver}
	t.Cleanup(func() {
		cancel()
		runtime.replayWait.Wait()
		_ = journal.Close()
		state.Close()
	})
	return runtime
}

func seedExposedReservation(t *testing.T, runtime *Runtime, now time.Time) (*cloudv1.RelayGrant, *cloudv1.RelayICEConfig) {
	t.Helper()
	grant, material := seedJournalOnlyExposed(t, runtime, now)
	if err := runtime.state.RegisterRelayGrant(context.Background(), grant, material); err != nil {
		t.Fatal(err)
	}
	return grant, material
}

func seedJournalOnlyExposed(t *testing.T, runtime *Runtime, now time.Time) (*cloudv1.RelayGrant, *cloudv1.RelayICEConfig) {
	t.Helper()
	request := &cloudv1.RelayReserveRequest{ReservationId: "00000000-0000-4000-8000-000000000201", AccountId: "00000000-0000-4000-8000-000000000202", DaemonId: "00000000-0000-4000-8000-000000000203", ClientId: "client", SessionId: "00000000-0000-4000-8000-000000000204", ObservedAt: timestamppb.New(now)}
	request.RequestDigest, _ = relayquota.ReserveRequestDigest(request)
	grant := runtimeTestGrant(t, request.GetReservationId(), request.GetSessionId(), now, 0)
	material, err := runtime.credentialDeriver.Material(grant)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.journalCreateRequested(request); err != nil {
		t.Fatal(err)
	}
	if err := runtime.journalApplyGrant(grant); err != nil {
		t.Fatal(err)
	}
	if err := runtime.journalMarkExposed(grant.GetReservationId()); err != nil {
		t.Fatal(err)
	}
	return grant, material
}

func runtimeTestGrant(t *testing.T, reservationID, sessionID string, now time.Time, sequence uint64) *cloudv1.RelayGrant {
	t.Helper()
	policySnapshot := &cloudv1.RelayPolicySnapshot{AccountId: "account", SubscriptionId: "subscription", PlanId: "plan", RelayEnabled: true, EdgeId: "edge", DaemonId: "daemon"}
	digest, err := relayquota.PolicyDigest(policySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	return &cloudv1.RelayGrant{ReservationId: reservationID, SessionId: sessionID, ReservedBytes: 1000, MaxRateBytesPerSecond: 1000, RenewSequence: sequence, AuthorizedUntil: timestamppb.New(now.Add(time.Minute)), PolicyDigest: digest, Policy: policySnapshot}
}

func settlementAck(settlement *cloudv1.RelaySettlement, grant *cloudv1.RelayGrant, settledAt time.Time) *cloudv1.RelaySettlementAck {
	recovery := uint64(0)
	if settlement.GetKind() == cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX {
		recovery = grant.GetReservedBytes()
	}
	return &cloudv1.RelaySettlementAck{ReservationId: settlement.GetReservationId(), Kind: settlement.GetKind(), IngressBytes: settlement.GetIngressBytes(), EgressBytes: settlement.GetEgressBytes(), RecoveryBytes: recovery, PolicyDigest: settlement.GetPolicyDigest(), ObservedAt: settlement.GetObservedAt(), SettledAt: timestamppb.New(settledAt), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED}
}
