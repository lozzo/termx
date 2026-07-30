package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/anytty/anytty/proto/apipb"
)

func TestApplicationCommandSpecMatchesCompiledDescriptor(t *testing.T) {
	contents, err := os.ReadFile("../" + defaultSpecPath)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := parseAndValidateSpec(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	want := (&apipb.CommandEnvelope{}).ProtoReflect().Descriptor().Oneofs().ByName("command").Fields().Len()
	if len(specs) != want {
		t.Fatalf("application command rows = %d, want descriptor count %d", len(specs), want)
	}
}

func TestApplicationCommandSpecRejectsMissingCommand(t *testing.T) {
	contents, err := os.ReadFile("../" + defaultSpecPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	_, err = parseAndValidateSpec(strings.NewReader(strings.Join(lines[:len(lines)-1], "\n")))
	if err == nil || !strings.Contains(err.Error(), "does not cover compiled command descriptor exactly once") {
		t.Fatalf("missing command error = %v", err)
	}
}

func TestApplicationCommandGenerationIsDeterministic(t *testing.T) {
	specs, err := loadAndValidateSpec("../" + defaultSpecPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := generateApplicationAPI(specs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateApplicationAPI(specs)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range first {
		if !bytes.Equal(content, second[name]) {
			t.Fatalf("generated output %s is nondeterministic", name)
		}
	}
}
