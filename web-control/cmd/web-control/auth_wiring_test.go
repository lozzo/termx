package main

import (
	"context"
	"testing"
)

func TestNewAccountServiceFromEnvRequiresTokenSecret(t *testing.T) {
	t.Setenv("TERMX_WEB_CONTROL_TOKEN_SECRET", "")
	_, err := newAccountServiceFromEnv(context.Background(), nil)
	if err == nil {
		t.Fatal("account service without token secret succeeded")
	}
}
