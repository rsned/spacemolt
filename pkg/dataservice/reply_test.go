package dataservice

import "testing"

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"nearest station from sol-3", FormatPlaintext},
		{"help", FormatPlaintext},
		{`{"query":"nearest"}`, FormatJSON},
		{`  {"query":"help"}  `, FormatJSON},
		{"", FormatPlaintext},
		{"{not-json-but-starts-with-brace", FormatJSON}, // detection is lexical
	}
	for _, c := range cases {
		if got := DetectFormat(c.in); got != c.want {
			t.Errorf("DetectFormat(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncateReply(t *testing.T) {
	short := "short"
	if got := TruncateReply(short); got != short {
		t.Errorf("short passthrough: got %q", got)
	}

	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}
	got := TruncateReply(string(long))
	if len(got) > 500 {
		t.Errorf("truncated reply exceeded 500 chars: %d", len(got))
	}
	suffix := "…[truncated]"
	if len(got) < len(suffix) || got[len(got)-len(suffix):] != suffix {
		t.Errorf("expected truncated suffix, got %q", got[len(got)-20:])
	}
}

func TestTruncateReply_Exact500(t *testing.T) {
	s := make([]byte, 500)
	for i := range s {
		s[i] = 'x'
	}
	got := TruncateReply(string(s))
	if got != string(s) {
		t.Errorf("exact-500 input should not be modified")
	}
}
