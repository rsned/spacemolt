package main

import "testing"

func TestScanBraceDepth(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantDepth int
		wantQuote bool
	}{
		{"empty", "", 0, false},
		{"balanced", "loop 5 { mine }", 0, false},
		{"open", "loop 5 { mine", 1, false},
		{"nested", "loop 2 { loop 3 { mine", 2, false},
		{"nested closed", "loop 2 { loop 3 { mine } }", 0, false},
		{"brace in double quote", `chat "}"`, 0, false},
		{"brace in single quote", `chat '}'`, 0, false},
		{"open brace in quote doesn't count", `chat "{" `, 0, false},
		{"unterminated quote", `chat "hi`, 0, true},
		{"close without open", "mine }", -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			depth, inQuote, err := scanBraceDepth(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if depth != tc.wantDepth {
				t.Errorf("depth: got %d, want %d", depth, tc.wantDepth)
			}
			if inQuote != tc.wantQuote {
				t.Errorf("inQuote: got %v, want %v", inQuote, tc.wantQuote)
			}
		})
	}
}
