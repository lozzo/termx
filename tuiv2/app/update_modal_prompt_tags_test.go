package app

import "testing"

func TestParsePromptTagsRejectsInvalidField(t *testing.T) {
	tags, err := parsePromptTags("env=test broken")
	if err == nil {
		t.Fatalf("expected parse error, got tags %#v", tags)
	}
	if _, ok := err.(inputError); !ok {
		t.Fatalf("expected inputError type, got %T", err)
	}
}

func TestFormatPromptTagsSortsKeys(t *testing.T) {
	text := formatPromptTags(map[string]string{"role": "dev", "env": "test"})
	if text != "env=test role=dev" {
		t.Fatalf("expected stable sorted tags, got %q", text)
	}
}
