package money

import "testing"

// ParseUSDC must return integer millionths with no rounding or coercion.
// The maximum is Lua's exact-integer limit, not Go's int64 limit.
func TestParseUSDCRejectsInvalidAmounts(t *testing.T) {
	for _, input := range []string{
		"", "0", "0.000000", "-1", "-0.000001", "+1",
		" 1", "1 ", "1e3", "NaN", "Inf", "1,000",
		".5", "1.", "1.0000001", "0.0000001",
		"9007199254.740992", "999999999999999999999999",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseUSDC(input); err == nil {
				t.Fatalf("ParseUSDC(%q) accepted an invalid amount", input)
			}
		})
	}
}

func TestParseUSDCExactBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int64
	}{
		{"0.000001", 1},
		{"1", 1_000_000},
		{"50.00", 50_000_000},
		{"9007199254.740991", 9_007_199_254_740_991},
	} {
		got, err := ParseUSDC(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("ParseUSDC(%q) = %d, %v; want %d", tc.input, got, err, tc.want)
		}
	}
}
