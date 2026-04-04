package app

import (
	"strings"
	"unicode"

	"github.com/lozzow/termx/tuiv2/modal"
)

func promptEditableValue(prompt *modal.PromptState) string {
	if field := promptEditableField(prompt); field != nil {
		return field.Value
	}
	if prompt == nil {
		return ""
	}
	return prompt.Value
}

func promptEditableCursor(prompt *modal.PromptState) int {
	if field := promptEditableField(prompt); field != nil {
		return field.Cursor
	}
	if prompt == nil {
		return 0
	}
	return prompt.Cursor
}

func setPromptEditableCursor(prompt *modal.PromptState, cursor int) bool {
	if field := promptEditableField(prompt); field != nil {
		clamped := cursor
		maxCursor := len([]rune(field.Value))
		if clamped < 0 {
			clamped = 0
		}
		if clamped > maxCursor {
			clamped = maxCursor
		}
		if field.Cursor == clamped {
			return false
		}
		field.Cursor = clamped
		return true
	}
	if prompt == nil {
		return false
	}
	clamped := cursor
	maxCursor := len([]rune(prompt.Value))
	if clamped < 0 {
		clamped = 0
	}
	if clamped > maxCursor {
		clamped = maxCursor
	}
	if prompt.Cursor == clamped {
		return false
	}
	prompt.Cursor = clamped
	return true
}

func promptEditableField(prompt *modal.PromptState) *modal.PromptField {
	if prompt == nil || !prompt.IsForm() {
		return nil
	}
	return prompt.ActivePromptField()
}

func movePromptFormField(prompt *modal.PromptState, delta int) bool {
	if prompt == nil || len(prompt.Fields) == 0 || delta == 0 {
		return false
	}
	next := prompt.ActiveField + delta
	if next < 0 {
		next = 0
	}
	if next >= len(prompt.Fields) {
		next = len(prompt.Fields) - 1
	}
	if prompt.ActiveField == next {
		return false
	}
	prompt.ActiveField = next
	return true
}

func promptFieldValue(prompt *modal.PromptState, key string) string {
	if prompt == nil {
		return ""
	}
	field := prompt.Field(key)
	if field == nil {
		return ""
	}
	return strings.TrimSpace(field.Value)
}

func promptCommandFromField(prompt *modal.PromptState) ([]string, error) {
	commandText := promptFieldValue(prompt, "command")
	if commandText == "" {
		return append([]string(nil), prompt.Command...), nil
	}
	command, err := parsePromptCommand(commandText)
	if err != nil {
		return nil, err
	}
	return command, nil
}

func promptWorkdirFromField(prompt *modal.PromptState) string {
	workdir := promptFieldValue(prompt, "workdir")
	if workdir != "" {
		return workdir
	}
	return strings.TrimSpace(prompt.Workdir)
}

func promptTagsFromField(prompt *modal.PromptState) (map[string]string, error) {
	if prompt == nil {
		return nil, nil
	}
	return parsePromptTags(promptFieldValue(prompt, "tags"))
}

func parsePromptCommand(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	var (
		args       []string
		current    []rune
		inSingle   bool
		inDouble   bool
		escaped    bool
		quotedPart bool
	)
	flush := func(force bool) {
		if len(current) == 0 && !quotedPart && !force {
			return
		}
		args = append(args, string(current))
		current = current[:0]
		quotedPart = false
	}
	for _, r := range value {
		switch {
		case escaped:
			current = append(current, r)
			escaped = false
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			quotedPart = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			quotedPart = true
		case !inSingle && !inDouble && unicode.IsSpace(r):
			flush(false)
		default:
			current = append(current, r)
		}
	}
	if escaped || inSingle || inDouble {
		return nil, inputError("invalid command syntax")
	}
	flush(false)
	if len(args) == 0 {
		return nil, nil
	}
	return args, nil
}
