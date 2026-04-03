package app

import (
	"sort"
	"strings"
)

type inputError string

func (e inputError) Error() string {
	return string(e)
}

func parsePromptTags(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\t' || r == ' '
	})
	tags := make(map[string]string, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		key, tagValue, ok := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		tagValue = strings.TrimSpace(tagValue)
		if !ok || key == "" {
			return nil, inputError("invalid tag syntax: " + part)
		}
		tags[key] = tagValue
	}
	return tags, nil
}

func formatPromptTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+tags[key])
	}
	return strings.Join(parts, " ")
}
