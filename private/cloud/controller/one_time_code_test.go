package controller

import (
	"bytes"
	"strings"
	"testing"
)

func TestOneTimeCodeUses128BitDomainSeparatedLocator(t *testing.T) {
	code, err := newOneTimeCode(bytes.NewReader(make([]byte, 16)), "MXA")
	if err != nil {
		t.Fatal(err)
	}
	if code != "MXA-0000-0000-0000-0000-0000-000000" {
		t.Fatalf("code = %q", code)
	}
	normalized, err := normalizeOneTimeCode(strings.ToLower(strings.ReplaceAll(code, "-", "")), "MXA")
	if err != nil || normalized != code {
		t.Fatalf("normalize = %q, %v", normalized, err)
	}
	if _, err := normalizeOneTimeCode(strings.Replace(code, "MXA", "MXD", 1), "MXA"); err == nil {
		t.Fatal("mobile activation accepted a daemon enrollment code")
	}
	if bytes.Equal(oneTimeCodeDigest(code), oneTimeCodeDigest(strings.Replace(code, "MXA", "MXD", 1))) {
		t.Fatal("domain-separated codes share a digest")
	}
}
