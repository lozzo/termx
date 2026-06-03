package app

import "testing"

func TestRuntimeContractsDoNotUseBubbleTea(t *testing.T) {
	var msg Msg = NoopMsg{}
	var effect Effect = NoopEffect{}
	if msg == nil {
		t.Fatal("expected msg contract")
	}
	if effect == nil {
		t.Fatal("expected effect contract")
	}
}
