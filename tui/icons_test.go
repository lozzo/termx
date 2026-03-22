package tui

import "testing"

func TestNormalizeIconSetNameDefaultsToUnicode(t *testing.T) {
	cases := map[string]string{
		"":          "unicode",
		"unicode":   "unicode",
		"UNICODE":   "unicode",
		"ascii":     "ascii",
		"nerd":      "nerd",
		"unknown":   "unicode",
		"  ascii  ": "ascii",
	}
	for input, want := range cases {
		if got := normalizeIconSetName(input); got != want {
			t.Fatalf("normalizeIconSetName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveIconSetProvidesDistinctASCIIAndNerdTokens(t *testing.T) {
	ascii := resolveIconSet("ascii")
	if ascii.token("run", ascii.Running) != "run" {
		t.Fatalf("expected ascii token to stay label-only, got %q", ascii.token("run", ascii.Running))
	}
	if ascii.countToken("pane", ascii.Pane, 2) != "pane:2" {
		t.Fatalf("expected ascii count token, got %q", ascii.countToken("pane", ascii.Pane, 2))
	}

	nerd := resolveIconSet("nerd")
	if got := nerd.token("run", nerd.Running); got == "run" || got == "" {
		t.Fatalf("expected nerd token to include icon, got %q", got)
	}
	if got := nerd.countToken("pane", nerd.Pane, 2); got == "pane:2" || got == "" {
		t.Fatalf("expected nerd count token to include icon, got %q", got)
	}
}
