package main

import (
	"errors"
	"flag"
	"io"
	"testing"
)

func TestParseOptionsSupportsHelpAndRejectsPositionalArguments(t *testing.T) {
	if _, err := parseOptions([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("--help error = %v, want flag.ErrHelp", err)
	}
	if _, err := parseOptions([]string{"unexpected"}, io.Discard); err == nil {
		t.Fatal("positional Controller argument was accepted")
	}
}

func TestParseOptionsKeepsOnlyTechnicalRelayLeaseTTL(t *testing.T) {
	config, err := parseOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if config.relayLeaseTTL <= 0 {
		t.Fatalf("relay lease TTL = %s, want positive", config.relayLeaseTTL)
	}
}
