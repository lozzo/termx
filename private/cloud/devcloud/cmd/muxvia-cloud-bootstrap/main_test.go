package main

import (
	"net/url"
	"testing"
)

func TestDSNWithSchemaKeepsTLSAndAddsSearchPath(t *testing.T) {
	value, err := dsnWithSchema("postgresql://user:secret@pooler.example:5432/postgres?sslmode=require", "muxvia_staging")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("sslmode") != "require" || parsed.Query().Get("search_path") != "muxvia_staging" {
		t.Fatalf("deployment DSN query = %v", parsed.Query())
	}
	if parsed.User.Username() != "user" {
		t.Fatalf("deployment DSN user = %q", parsed.User.Username())
	}
}

func TestBootstrapOriginsRequireCleanHTTPSOrigin(t *testing.T) {
	if err := validateOrigin("https://cn1.edge.muxvia.com:41102"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"http://muxvia.com", "https://user@muxvia.com", "https://muxvia.com/path", "https://muxvia.com?query=1"} {
		if err := validateOrigin(value); err == nil {
			t.Fatalf("invalid origin %q was accepted", value)
		}
	}
}

func TestBootstrapCreemOptions(t *testing.T) {
	if err := validateCreemOptions("test", "https://muxvia.com/account?payment=return"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		environment string
		successURL  string
	}{
		{environment: "sandbox", successURL: "https://muxvia.com/account"},
		{environment: "test", successURL: "http://muxvia.com/account"},
		{environment: "production", successURL: "https://user@muxvia.com/account"},
	} {
		if err := validateCreemOptions(test.environment, test.successURL); err == nil {
			t.Fatalf("invalid Creem options were accepted: %+v", test)
		}
	}
}
