package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The REPL splits a command line on whitespace, so forum_reply's
// strings.Join(parts[2:], " ") could never carry a newline: every
// multi-paragraph post collapsed into one run-on line, and there was no way to
// write a readable bug report from play_as at all.
func TestForumBodyFromFilePreservesNewlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "post.md")
	want := "First paragraph.\n\nSecond paragraph.\n- a bullet\n- another"
	if err := os.WriteFile(path, []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"--file=" + path},
		{"--file", path},
	} {
		got, err := forumBody(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if got != want {
			t.Errorf("%v: body = %q, want %q", args, got, want)
		}
		if !strings.Contains(got, "\n\n") {
			t.Errorf("%v: the blank line between paragraphs must survive", args)
		}
	}
}

// The inline form is what everyone already uses; it must keep working.
func TestForumBodyInlineStillJoins(t *testing.T) {
	got, err := forumBody([]string{"seen", "it", "too"})
	if err != nil {
		t.Fatalf("inline: %v", err)
	}
	if got != "seen it too" {
		t.Errorf("body = %q, want %q", got, "seen it too")
	}
}

// A trailing newline is an artefact of writing the file, not part of the post.
// Interior blank lines are the author's and must not be touched.
func TestForumBodyTrimsOnlyTrailingNewlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "post.md")
	if err := os.WriteFile(path, []byte("line one\n\nline two\n\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := forumBody([]string{"--file=" + path})
	if err != nil {
		t.Fatalf("forumBody: %v", err)
	}
	if got != "line one\n\nline two" {
		t.Errorf("body = %q, want %q", got, "line one\n\nline two")
	}
}

// Posting nothing is always a mistake, and an empty post cannot be edited away
// once it is on someone else's thread.
func TestForumBodyRefusesEmpty(t *testing.T) {
	if _, err := forumBody(nil); err == nil {
		t.Error("no content and no --file must be an error")
	}
	if _, err := forumBody([]string{"   "}); err == nil {
		t.Error("whitespace-only inline content must be an error")
	}

	path := filepath.Join(t.TempDir(), "blank.md")
	if err := os.WriteFile(path, []byte("\n\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := forumBody([]string{"--file=" + path}); err == nil {
		t.Error("a whitespace-only file must be an error, not an empty post")
	}
}

func TestForumBodyMissingFileIsNamed(t *testing.T) {
	_, err := forumBody([]string{"--file=/nonexistent/nope.md"})
	if err == nil {
		t.Fatal("an unreadable file must be an error")
	}
	if !strings.Contains(err.Error(), "nope.md") {
		t.Errorf("error %q should name the file", err)
	}
}
