package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuditCursorRoundTripAndRejectsInvalidValues(t *testing.T) {
	at := time.Date(2026, 7, 26, 12, 34, 56, 789, time.FixedZone("CST", 8*60*60))
	id := uuid.NewString()
	decodedAt, decodedID, err := decodeAuditCursor(encodeAuditCursor(at, id))
	if err != nil {
		t.Fatal(err)
	}
	if !decodedAt.Equal(at) || decodedID != id {
		t.Fatalf("decoded cursor = %s %s", decodedAt, decodedID)
	}
	if _, _, err := decodeAuditCursor("not-a-cursor"); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
}
