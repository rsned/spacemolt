package main

import (
	"bytes"
	"log"
	"testing"
)

// TestLogLineWriter verifies the decision-stream adapter stamps every complete
// line with the logger's prefix and holds an unterminated remainder until its
// newline arrives (so a chunked fmt.Fprintf never splits a line's prefix).
func TestLogLineWriter(t *testing.T) {
	var buf bytes.Buffer
	// No timestamp flags so the assertion is stable.
	l := log.New(&buf, "[worker:engineer-4] ", 0)
	w := &logLineWriter{l: l}

	// Two complete lines in one write.
	if _, err := w.Write([]byte("freight: skip A\nfreight: skip B\n")); err != nil {
		t.Fatal(err)
	}
	// A line delivered in two chunks (partial, then the newline).
	if _, err := w.Write([]byte("freight: best ")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "[worker:engineer-4] freight: skip A\n[worker:engineer-4] freight: skip B\n" {
		t.Fatalf("after complete lines, got:\n%q", buf.String())
	}
	if _, err := w.Write([]byte("C to sol\n")); err != nil {
		t.Fatal(err)
	}

	want := "[worker:engineer-4] freight: skip A\n" +
		"[worker:engineer-4] freight: skip B\n" +
		"[worker:engineer-4] freight: best C to sol\n"
	if buf.String() != want {
		t.Fatalf("final mismatch:\ngot:  %q\nwant: %q", buf.String(), want)
	}
}
