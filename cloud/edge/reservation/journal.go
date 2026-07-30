// Package reservation owns the Edge's durable unsettled Relay facts.
package reservation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/relayquota"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"
)

const MaxDurableRecords = 100000

var recordsBucket = []byte("relay-reservations-v1")

var (
	ErrConflict    = errors.New("Relay journal payload conflicts with durable record")
	ErrJournalFull = errors.New("Relay journal reached its durable unsettled limit")
	ErrStage       = errors.New("Relay journal transition is invalid")
)

type Journal struct {
	database *bolt.DB
	limit    int
}

func Open(path string, timeout time.Duration) (*Journal, error) {
	return OpenWithLimit(path, timeout, MaxDurableRecords)
}

func OpenWithLimit(path string, timeout time.Duration, limit int) (*Journal, error) {
	path = strings.TrimSpace(path)
	if path == "" || timeout <= 0 || limit <= 0 || limit > MaxDurableRecords {
		return nil, errors.New("Relay journal path, timeout, and bounded record limit are required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create Relay journal directory: %w", err)
	}
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("open Relay journal: %w", err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(recordsBucket)
		return err
	}); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize Relay journal: %w", err)
	}
	return &Journal{database: database, limit: limit}, nil
}

func (journal *Journal) CreateRequested(request *cloudv1.RelayReserveRequest) error {
	if err := validateReserveRequest(request); err != nil {
		return err
	}
	record := &cloudv1.RelayJournalRecord{SchemaVersion: 1, Stage: cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_REQUESTED, ReserveRequest: proto.Clone(request).(*cloudv1.RelayReserveRequest)}
	return journal.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(recordsBucket)
		key := []byte(request.GetReservationId())
		if existing := bucket.Get(key); existing != nil {
			return equalRecord(existing, record)
		}
		if bucket.Stats().KeyN >= journal.limit {
			return ErrJournalFull
		}
		return putRecord(bucket, key, record)
	})
}

func (journal *Journal) ApplyGrant(grant *cloudv1.RelayGrant) error {
	if err := validateGrant(grant); err != nil {
		return err
	}
	return journal.update(grant.GetReservationId(), func(record *cloudv1.RelayJournalRecord) error {
		if record.GetReserveRequest().GetSessionId() != grant.GetSessionId() {
			return ErrConflict
		}
		if record.GetGrant() != nil {
			if proto.Equal(record.GetGrant(), grant) {
				return nil
			}
			return ErrConflict
		}
		if record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_REQUESTED {
			return ErrStage
		}
		record.Grant = proto.Clone(grant).(*cloudv1.RelayGrant)
		record.Stage = cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_HELD_UNEXPOSED
		return nil
	})
}

// DropRequested removes a request only after Controller definitively rejected it.
// An uncertain transport failure must leave REQUESTED durable for replay.
func (journal *Journal) DropRequested(reservationID string, requestDigest []byte) error {
	return journal.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(recordsBucket)
		key := []byte(strings.TrimSpace(reservationID))
		payload := bucket.Get(key)
		if payload == nil {
			return nil
		}
		record, err := decodeRecord(key, payload)
		if err != nil {
			return err
		}
		if record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_REQUESTED || !relayquota.EqualDigest(record.GetReserveRequest().GetRequestDigest(), requestDigest) {
			return ErrStage
		}
		return bucket.Delete(key)
	})
}

func (journal *Journal) MarkExposed(reservationID string) error {
	return journal.update(reservationID, func(record *cloudv1.RelayJournalRecord) error {
		switch record.GetStage() {
		case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_HELD_UNEXPOSED:
			record.Stage = cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED
			return nil
		case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED:
			return nil
		default:
			return ErrStage
		}
	})
}

func (journal *Journal) MarkRenewPending(reservationID string, sequence uint64) error {
	return journal.update(reservationID, func(record *cloudv1.RelayJournalRecord) error {
		if record.GetStage() == cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING {
			if record.GetPendingRenewSequence() == sequence {
				return nil
			}
			return ErrConflict
		}
		if record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED || record.GetGrant() == nil || sequence != record.GetGrant().GetRenewSequence()+1 {
			return ErrStage
		}
		record.Stage = cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING
		record.PendingRenewSequence = sequence
		return nil
	})
}

func (journal *Journal) ApplyRenewedGrant(grant *cloudv1.RelayGrant) error {
	if err := validateGrant(grant); err != nil {
		return err
	}
	return journal.update(grant.GetReservationId(), func(record *cloudv1.RelayJournalRecord) error {
		if record.GetStage() == cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED && proto.Equal(record.GetGrant(), grant) {
			return nil
		}
		if record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING || record.GetPendingRenewSequence() != grant.GetRenewSequence() || record.GetGrant().GetSessionId() != grant.GetSessionId() || !relayquota.EqualDigest(record.GetGrant().GetPolicyDigest(), grant.GetPolicyDigest()) {
			return ErrStage
		}
		record.Grant = proto.Clone(grant).(*cloudv1.RelayGrant)
		record.PendingRenewSequence = 0
		record.Stage = cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED
		return nil
	})
}

func (journal *Journal) MarkClosing(reservationID string) error {
	return journal.update(reservationID, func(record *cloudv1.RelayJournalRecord) error {
		switch record.GetStage() {
		case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED, cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING:
			record.Stage = cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING
			return nil
		case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING:
			return nil
		default:
			return ErrStage
		}
	})
}

func (journal *Journal) PutSettlement(settlement *cloudv1.RelaySettlement) error {
	if err := validateSettlement(settlement); err != nil {
		return err
	}
	return journal.update(settlement.GetReservationId(), func(record *cloudv1.RelayJournalRecord) error {
		if record.GetSettlement() != nil {
			if proto.Equal(record.GetSettlement(), settlement) {
				return nil
			}
			return ErrConflict
		}
		if record.GetGrant() == nil || !relayquota.EqualDigest(record.GetGrant().GetPolicyDigest(), settlement.GetPolicyDigest()) {
			return ErrConflict
		}
		if settlement.GetKind() == cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT {
			if record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING && record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_HELD_UNEXPOSED {
				return ErrStage
			}
			if settlement.GetIngressBytes() > record.GetGrant().GetReservedBytes() || settlement.GetEgressBytes() > record.GetGrant().GetReservedBytes()-settlement.GetIngressBytes() {
				return errors.New("exact settlement exceeds reserved bytes")
			}
		} else if record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED && record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING && record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING {
			return ErrStage
		}
		record.Settlement = proto.Clone(settlement).(*cloudv1.RelaySettlement)
		record.Stage = cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE
		return nil
	})
}

// Ack deletes only a locally verifiable terminal fact. Authoritative recovery
// may beat an exact close, so RECOVERY_MAX is checked against the durable grant.
func (journal *Journal) Ack(ack *cloudv1.RelaySettlementAck) error {
	if ack == nil || strings.TrimSpace(ack.GetReservationId()) == "" || ack.GetObservedAt() == nil || ack.GetSettledAt() == nil || ack.GetObservedAt().CheckValid() != nil || ack.GetSettledAt().CheckValid() != nil ||
		(ack.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED && ack.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY && ack.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL) {
		return errors.New("committed Relay settlement ACK is required")
	}
	return journal.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(recordsBucket)
		key := []byte(ack.GetReservationId())
		payload := bucket.Get(key)
		if payload == nil {
			return nil
		}
		record, err := decodeRecord(key, payload)
		if err != nil {
			return err
		}
		if !relayquota.EqualDigest(record.GetGrant().GetPolicyDigest(), ack.GetPolicyDigest()) {
			return ErrConflict
		}
		valid := false
		if ack.GetKind() == cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX {
			valid = ack.GetIngressBytes() == 0 && ack.GetEgressBytes() == 0 && ack.GetRecoveryBytes() == record.GetGrant().GetReservedBytes()
		} else if record.GetSettlement() != nil && record.GetSettlement().GetKind() == cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT {
			valid = ack.GetRecoveryBytes() == 0 && ack.GetIngressBytes() == record.GetSettlement().GetIngressBytes() && ack.GetEgressBytes() == record.GetSettlement().GetEgressBytes() && ack.GetObservedAt().AsTime().Equal(record.GetSettlement().GetObservedAt().AsTime())
		}
		if !valid {
			return ErrConflict
		}
		return bucket.Delete(key)
	})
}

func (journal *Journal) Records(limit int) ([]*cloudv1.RelayJournalRecord, error) {
	if journal == nil || journal.database == nil || limit <= 0 {
		return nil, errors.New("open Relay journal and positive batch limit are required")
	}
	result := make([]*cloudv1.RelayJournalRecord, 0, limit)
	err := journal.database.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(recordsBucket).Cursor()
		for key, value := cursor.First(); key != nil && len(result) < limit; key, value = cursor.Next() {
			record, err := decodeRecord(key, value)
			if err != nil {
				return err
			}
			result = append(result, record)
		}
		return nil
	})
	return result, err
}

func (journal *Journal) Record(reservationID string) (*cloudv1.RelayJournalRecord, bool, error) {
	var record *cloudv1.RelayJournalRecord
	err := journal.database.View(func(tx *bolt.Tx) error {
		payload := tx.Bucket(recordsBucket).Get([]byte(reservationID))
		if payload == nil {
			return nil
		}
		var err error
		record, err = decodeRecord([]byte(reservationID), payload)
		return err
	})
	return record, record != nil, err
}

func (journal *Journal) Len() (int, error) {
	count := 0
	err := journal.database.View(func(tx *bolt.Tx) error {
		count = tx.Bucket(recordsBucket).Stats().KeyN
		return nil
	})
	return count, err
}

func (journal *Journal) Close() error {
	if journal == nil || journal.database == nil {
		return nil
	}
	err := journal.database.Close()
	journal.database = nil
	return err
}

func (journal *Journal) update(reservationID string, mutate func(*cloudv1.RelayJournalRecord) error) error {
	if journal == nil || journal.database == nil || strings.TrimSpace(reservationID) == "" {
		return errors.New("open Relay journal and reservation ID are required")
	}
	return journal.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(recordsBucket)
		key := []byte(reservationID)
		payload := bucket.Get(key)
		if payload == nil {
			return errors.New("Relay journal record is unavailable")
		}
		record, err := decodeRecord(key, payload)
		if err != nil {
			return err
		}
		if err := mutate(record); err != nil {
			return err
		}
		return putRecord(bucket, key, record)
	})
}

func validateReserveRequest(request *cloudv1.RelayReserveRequest) error {
	if request == nil || strings.TrimSpace(request.GetReservationId()) == "" || request.GetObservedAt() == nil || request.GetObservedAt().CheckValid() != nil {
		return errors.New("complete Relay reserve request is required")
	}
	digest, err := relayquota.ReserveRequestDigest(request)
	if err != nil || !relayquota.EqualDigest(digest, request.GetRequestDigest()) {
		return errors.New("Relay reserve request digest is invalid")
	}
	return nil
}

func validateGrant(grant *cloudv1.RelayGrant) error {
	if grant == nil || grant.GetPolicy() == nil || grant.GetAuthorizedUntil() == nil || grant.GetAuthorizedUntil().CheckValid() != nil || grant.GetReservedBytes() == 0 || grant.GetMaxRateBytesPerSecond() == 0 {
		return errors.New("complete Relay grant is required")
	}
	digest, err := relayquota.PolicyDigest(grant.GetPolicy())
	if err != nil || !relayquota.EqualDigest(digest, grant.GetPolicyDigest()) {
		return errors.New("Relay grant policy digest is invalid")
	}
	return nil
}

func validateSettlement(settlement *cloudv1.RelaySettlement) error {
	if settlement == nil || strings.TrimSpace(settlement.GetReservationId()) == "" || settlement.GetObservedAt() == nil || settlement.GetObservedAt().CheckValid() != nil || len(settlement.GetPolicyDigest()) != 32 {
		return errors.New("complete Relay settlement is required")
	}
	if settlement.GetKind() == cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX && (settlement.GetIngressBytes() != 0 || settlement.GetEgressBytes() != 0) {
		return errors.New("RECOVERY_MAX cannot contain exact counters")
	}
	if settlement.GetKind() != cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT && settlement.GetKind() != cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX {
		return errors.New("Relay settlement kind is invalid")
	}
	return nil
}

func equalRecord(payload []byte, expected *cloudv1.RelayJournalRecord) error {
	actual := &cloudv1.RelayJournalRecord{}
	if err := proto.Unmarshal(payload, actual); err != nil {
		return err
	}
	if proto.Equal(actual, expected) {
		return nil
	}
	return ErrConflict
}

func decodeRecord(key, payload []byte) (*cloudv1.RelayJournalRecord, error) {
	record := &cloudv1.RelayJournalRecord{}
	if err := proto.Unmarshal(payload, record); err != nil {
		return nil, fmt.Errorf("decode Relay journal record %q: %w", key, err)
	}
	if record.GetSchemaVersion() != 1 || record.GetReserveRequest().GetReservationId() != string(key) {
		return nil, fmt.Errorf("Relay journal record %q is invalid", key)
	}
	return record, nil
}

func putRecord(bucket *bolt.Bucket, key []byte, record *cloudv1.RelayJournalRecord) error {
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
	if err != nil {
		return err
	}
	return bucket.Put(key, payload)
}
