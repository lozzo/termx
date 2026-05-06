package config

import "testing"

func TestNormalizeDefaultsRemoteMode(t *testing.T) {
	cfg := Normalize(Config{})
	if cfg.Mode != "both" {
		t.Fatalf("expected default mode both, got %q", cfg.Mode)
	}
	if !ModeIncludesLocal(cfg.Mode) || !ModeIncludesOnline(cfg.Mode) {
		t.Fatalf("default mode should include local and online: %q", cfg.Mode)
	}
}

func TestNormalizeAcceptsRemoteModes(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: " local ", want: "local"},
		{input: "ONLINE", want: "online"},
		{input: "both", want: "both"},
	} {
		cfg := Normalize(Config{Mode: tc.input})
		if cfg.Mode != tc.want {
			t.Fatalf("Normalize(%q).Mode = %q, want %q", tc.input, cfg.Mode, tc.want)
		}
	}
}

func TestValidateRejectsInvalidRemoteMode(t *testing.T) {
	cfg := Normalize(Config{Enabled: true, DataDir: t.TempDir(), Mode: "bad"})
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid mode validation error")
	}
}

func TestNormalizeTrimsLANIPs(t *testing.T) {
	cfg := Normalize(Config{LANIPs: []string{" 192.168.0.0/16 ", "", "10.0.0.8"}})
	if len(cfg.LANIPs) != 2 || cfg.LANIPs[0] != "192.168.0.0/16" || cfg.LANIPs[1] != "10.0.0.8" {
		t.Fatalf("unexpected LAN IPs: %#v", cfg.LANIPs)
	}
}
