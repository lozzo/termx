package main

import (
	"bytes"
	"testing"
)

func TestWriteCLITableAlignsUnicodeByTerminalCells(t *testing.T) {
	var output bytes.Buffer
	err := writeCLITable(&output, []string{"ID", "LABEL", "STATE"}, [][]string{
		{"a", "中文", "on"},
		{"long", "x", "off"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "ID    LABEL  STATE\n" +
		"a     中文   on\n" +
		"long  x      off\n"
	if output.String() != want {
		t.Fatalf("table output:\n%q\nwant:\n%q", output.String(), want)
	}
}

func TestWriteCLIOutputSanitizesCellsAndAlignsFields(t *testing.T) {
	var output bytes.Buffer
	if err := writeCLITable(&output, nil, [][]string{{"first\nline", ""}, {"二", "ok"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "first line  -\n二          ok\n"; got != want {
		t.Fatalf("table output = %q, want %q", got, want)
	}

	output.Reset()
	if err := writeCLIFields(&output,
		cliField{Label: "ID", Value: "studio"},
		cliField{Label: "连接方式", Value: "direct"},
	); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "ID        studio\n连接方式  direct\n"; got != want {
		t.Fatalf("field output = %q, want %q", got, want)
	}
}
