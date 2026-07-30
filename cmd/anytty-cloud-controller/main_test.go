package main

import (
	"errors"
	"flag"
	"io"
	"net/netip"
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

func TestParseOptionsTrustedProxyCIDRsAreExplicitAndRepeatable(t *testing.T) {
	config, err := parseOptions([]string{"--trusted-proxy-cidr=127.0.0.0/8", "--trusted-proxy-cidr=2001:db8::/32"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("2001:db8::/32")}
	if len(config.trustedProxyCIDRs) != len(want) || config.trustedProxyCIDRs[0] != want[0] || config.trustedProxyCIDRs[1] != want[1] {
		t.Fatalf("trusted proxy CIDRs = %v", config.trustedProxyCIDRs)
	}
	config, err = parseOptions(nil, io.Discard)
	if err != nil || len(config.trustedProxyCIDRs) != 0 {
		t.Fatalf("default trusted proxy CIDRs = %v, err=%v", config.trustedProxyCIDRs, err)
	}
	if _, err := parseOptions([]string{"--trusted-proxy-cidr=not-a-cidr"}, io.Discard); err == nil {
		t.Fatal("invalid trusted proxy CIDR was accepted")
	}
}
