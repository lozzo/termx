package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestR423HistoryHarnessDumpsScreenBackedProjection(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"-cols", "12", "-rows", "1"}, strings.NewReader("sealed\r\ncurrent"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"# committed physical rows",
		`ROW section=committed`,
		`text="sealed"`,
		"# current-main physical rows",
		`text="current"`,
		"# projected logical lines",
		`row_ids=[`,
		`PROW index=`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("harness output missing %q\n%s", want, out)
		}
	}
}
