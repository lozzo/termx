package termxcorev2

import (
	"reflect"
	"testing"
)

func TestParseSGRParamsKeepsLegacySeparatorsAndInvalidParts(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []int
	}{
		{name: "empty", text: "", want: nil},
		{name: "semicolon", text: "38;5;196", want: []int{38, 5, 196}},
		{name: "colon", text: "38:5:196", want: []int{38, 5, 196}},
		{name: "empty part", text: "1;;31", want: []int{1, 0, 31}},
		{name: "invalid part skipped", text: "1;bad;32", want: []int{1, 32}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseSGRParams(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseSGRParams(%q) = %#v, want %#v", tt.text, got, tt.want)
			}
		})
	}
}
