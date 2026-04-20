package termx

import (
	"math"
	"strconv"
	"testing"
)

func TestParseNumericStringRejectsNonNumericWithoutChangingOrderingSemantics(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   uint64
		wantOK bool
	}{
		{name: "empty", input: "", want: 0, wantOK: false},
		{name: "spaces", input: "   ", want: 0, wantOK: false},
		{name: "zero", input: "0", want: 0, wantOK: false},
		{name: "numeric", input: "42", want: 42, wantOK: true},
		{name: "numeric_with_spaces", input: "  42  ", want: 42, wantOK: true},
		{name: "alpha_prefix", input: "bench-0001", want: 0, wantOK: false},
		{name: "alpha_suffix", input: "42a", want: 0, wantOK: false},
		{name: "sign", input: "-1", want: 0, wantOK: false},
		{name: "max_uint64", input: strconv.FormatUint(math.MaxUint64, 10), want: math.MaxUint64, wantOK: true},
		{name: "overflow", input: "18446744073709551616", want: 0, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseNumericString(tc.input)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("parseNumericString(%q) = (%d, %v), want (%d, %v)", tc.input, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestLessNumericStringKeepsNumericAwareOrdering(t *testing.T) {
	if !lessNumericString("2", "10") {
		t.Fatal("expected numeric strings to sort numerically")
	}
	if lessNumericString("bench-2", "bench-10") {
		t.Fatal("expected non-numeric strings to keep lexical ordering")
	}
}
