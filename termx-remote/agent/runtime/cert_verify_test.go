package runtime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOfferWithValidCertIsAccepted(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error != "" {
		t.Fatalf("valid cert offer returned error: %#v", answer)
	}
	if answerer.calls != 1 {
		t.Fatalf("valid cert offer did not reach answer generation, calls=%d", answerer.calls)
	}
}

func TestOfferWithExpiredCertIsRejected(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer = resignCloudOfferWithCert(t, manager, offer, certFixtureOptions{
		IssuedAt:  time.Now().UTC().Add(-2 * time.Hour),
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	})

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "expired") {
		t.Fatalf("expected expired cert rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("expired cert reached answer generation, calls=%d", answerer.calls)
	}
}

func TestOfferWithWrongMachineIDIsRejected(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer = resignCloudOfferWithCert(t, manager, offer, certFixtureOptions{
		MachineID: "other-machine",
	})

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "machine_id") {
		t.Fatalf("expected machine_id rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("wrong-machine cert reached answer generation, calls=%d", answerer.calls)
	}
}

func TestOfferWithInvalidSignatureIsRejected(t *testing.T) {
	manager, answerer, offer := newCloudOfferFixture(t)
	offer.AppCertificate = []byte(strings.Replace(string(offer.AppCertificate), `"signature":"`, `"signature":"AAAA`, 1))

	answer := manager.answerCloudOffer(context.Background(), offer, nil)

	if answer.Error == "" || !strings.Contains(answer.Error, "signature") {
		t.Fatalf("expected invalid signature rejection, got %#v", answer)
	}
	if answerer.calls != 0 {
		t.Fatalf("invalid cert signature reached answer generation, calls=%d", answerer.calls)
	}
}
