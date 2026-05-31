package main

import (
	"strings"
	"testing"
)

func TestParseFlagArgs_DoubleDash(t *testing.T) {
	got, err := parseFlagArgs([]string{"--page_size", "5", "--category", "faction"}, "page_size", "category")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["page_size"] != 5 {
		t.Errorf("page_size: want int 5, got %#v", got["page_size"])
	}
	if got["category"] != "faction" {
		t.Errorf("category: want \"faction\", got %#v", got["category"])
	}
}

// TestParseFlagArgs_SingleDashLongFlagErrors covers the reported case where
// `get_action_log --page_size 5 -category faction` silently dropped the
// single-dash -category. A single-dash long flag must now error, pointing the
// operator at the two-dash form.
func TestParseFlagArgs_SingleDashLongFlagErrors(t *testing.T) {
	_, err := parseFlagArgs([]string{"--page_size", "5", "-category", "faction"}, "page_size", "category")
	if err == nil {
		t.Fatal("single-dash -category should error")
	}
	if !strings.Contains(err.Error(), "--category") {
		t.Errorf("error should suggest the two-dash form, got: %v", err)
	}
}

// TestParseFlagArgs_SingleDashErrorsEvenIfUnknownKey errors regardless of
// whether the flag name is a recognized option — the dash count is the issue.
func TestParseFlagArgs_SingleDashErrorsEvenIfUnknownKey(t *testing.T) {
	if _, err := parseFlagArgs([]string{"-bogus", "value"}, "page"); err == nil {
		t.Error("single-dash -bogus should error")
	}
}

// TestParseFlagArgs_NegativeNumberNotAFlag confirms a single-dash token that is
// a negative number (a value, not a flag) does not trigger the error.
func TestParseFlagArgs_NegativeNumberNotAFlag(t *testing.T) {
	got, err := parseFlagArgs([]string{"--offset", "-5"}, "offset")
	if err != nil {
		t.Fatalf("negative value should not error: %v", err)
	}
	if got["offset"] != -5 {
		t.Errorf("negative value should be consumed by --offset: %#v", got)
	}
	// A bare -5 with no preceding flag is ignored, not treated as a flag.
	got, err = parseFlagArgs([]string{"-5"}, "offset")
	if err != nil {
		t.Fatalf("bare -5 should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("bare -5 should be ignored, got %#v", got)
	}
}

func TestParseFlagArgs_BareBooleanAndEquals(t *testing.T) {
	got, err := parseFlagArgs([]string{"--detail", "--page=3"}, "detail", "page")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["detail"] != "true" {
		t.Errorf("bare --detail should be \"true\", got %#v", got["detail"])
	}
	if got["page"] != 3 {
		t.Errorf("page: want 3, got %#v", got["page"])
	}
}
