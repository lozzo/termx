package services

import "testing"

func TestRequestIDValid(t *testing.T) {
	if !RequestID(1).Valid() {
		t.Fatal("expected non-zero request id to be valid")
	}
	if RequestID(0).Valid() {
		t.Fatal("expected zero request id to be invalid")
	}
}
