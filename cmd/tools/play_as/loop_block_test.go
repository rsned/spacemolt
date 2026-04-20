package main

import "testing"

func TestParseStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // expected Raw values, in order
	}{
		{"empty", "", nil},
		{"whitespace only", "  \n\t \n", nil},
		{"single newline-separated", "travel sol_belt\nmine\nrefuel",
			[]string{"travel sol_belt", "mine", "refuel"}},
		{"single semicolon-separated", "travel sol_belt; mine; refuel",
			[]string{"travel sol_belt", "mine", "refuel"}},
		{"mixed separators", "travel sol_belt\nmine; mine\nrefuel",
			[]string{"travel sol_belt", "mine", "mine", "refuel"}},
		{"trailing semicolon", "mine; refuel;",
			[]string{"mine", "refuel"}},
		{"semicolon in double quote", `chat "hi; there"; mine`,
			[]string{`chat "hi; there"`, "mine"}},
		{"semicolon in single quote", `chat 'hi; there'; mine`,
			[]string{`chat 'hi; there'`, "mine"}},
		{"newline in double quote", "chat \"hi\nthere\"\nmine",
			[]string{"chat \"hi\nthere\"", "mine"}},
		{"line comment stripped", "# first\nmine # trailing\nrefuel",
			[]string{"mine", "refuel"}},
		{"hash in quote preserved", `chat "#1 fan"; mine`,
			[]string{`chat "#1 fan"`, "mine"}},
		{"nested block kept intact", "loop 3 { mine; refuel }; dock",
			[]string{"loop 3 { mine; refuel }", "dock"}},
		{"nested block with newlines", "loop 3 {\n  mine\n  refuel\n}\ndock",
			[]string{"loop 3 {\n  mine\n  refuel\n}", "dock"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStatements(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d statements, want %d: %v", len(got), len(tc.want), rawOf(got))
			}
			for i, s := range got {
				if s.Raw != tc.want[i] {
					t.Errorf("stmt %d: Raw = %q, want %q", i, s.Raw, tc.want[i])
				}
			}
		})
	}
}

func TestParseStatements_UnbalancedBraces(t *testing.T) {
	if _, err := parseStatements("loop 3 { mine"); err == nil {
		t.Fatal("expected error for unbalanced braces, got nil")
	}
	if _, err := parseStatements("mine }"); err == nil {
		t.Fatal("expected error for stray close brace, got nil")
	}
}

func TestParseStatements_TokensPopulated(t *testing.T) {
	got, err := parseStatements("sell iron_ore 5")
	if err != nil || len(got) != 1 {
		t.Fatalf("parse failed: %v, got %d stmts", err, len(got))
	}
	want := []string{"sell", "iron_ore", "5"}
	if len(got[0].Tokens) != len(want) {
		t.Fatalf("Tokens len = %d, want %d", len(got[0].Tokens), len(want))
	}
	for i, tok := range got[0].Tokens {
		if tok != want[i] {
			t.Errorf("Tokens[%d] = %q, want %q", i, tok, want[i])
		}
	}
}

func rawOf(ss []Statement) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Raw
	}
	return out
}

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
