package securetransport

import (
	"crypto/x509"
	"net/url"
	"testing"
)

func TestEdgeIDFromCertificateRequiresCanonicalIdentityURI(t *testing.T) {
	identity, err := EdgeIdentityURI("edge-1")
	if err != nil {
		t.Fatalf("create Edge identity URI: %v", err)
	}
	edgeID, err := EdgeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{identity}})
	if err != nil {
		t.Fatalf("parse canonical Edge identity URI: %v", err)
	}
	if edgeID != "edge-1" {
		t.Fatalf("Edge ID = %q, want edge-1", edgeID)
	}

	queryIdentity := *identity
	queryIdentity.RawQuery = "alternate=true"
	if _, err := EdgeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{&queryIdentity}}); err == nil {
		t.Fatal("Edge identity URI with query was accepted")
	}
	userIdentity := *identity
	userIdentity.User = url.User("unexpected")
	if _, err := EdgeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{&userIdentity}}); err == nil {
		t.Fatal("Edge identity URI with userinfo was accepted")
	}
}

func TestEdgeIDFromCertificateRejectsMissingAndDuplicateIdentity(t *testing.T) {
	if _, err := EdgeIDFromCertificate(&x509.Certificate{}); err == nil {
		t.Fatal("certificate without Edge identity URI was accepted")
	}
	identity, err := EdgeIdentityURI("edge-1")
	if err != nil {
		t.Fatalf("create Edge identity URI: %v", err)
	}
	if _, err := EdgeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{identity, identity}}); err == nil {
		t.Fatal("certificate with duplicate Edge identity URIs was accepted")
	}
}
