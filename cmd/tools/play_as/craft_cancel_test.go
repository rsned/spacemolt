package main

import (
	"reflect"
	"testing"
)

func TestCraftCancelPayload(t *testing.T) {
	cases := []struct {
		name      string
		craftArgs []string
		flags     map[string]string
		want      map[string]any
		wantErr   bool
	}{
		{
			name:      "positional job_id after cancel",
			craftArgs: []string{"cancel", "ec20a8c609ac66fc0adae8ec47f61369"},
			flags:     map[string]string{},
			want:      map[string]any{"action": "cancel", "job_id": "ec20a8c609ac66fc0adae8ec47f61369"},
		},
		{
			name:      "--job_id flag",
			craftArgs: []string{"cancel"},
			flags:     map[string]string{"job_id": "abc123"},
			want:      map[string]any{"action": "cancel", "job_id": "abc123"},
		},
		{
			name:      "action=cancel with --job_id, no positional",
			craftArgs: nil,
			flags:     map[string]string{"action": "cancel", "job_id": "abc123"},
			want:      map[string]any{"action": "cancel", "job_id": "abc123"},
		},
		{
			name:      "--job_ids csv takes precedence and trims",
			craftArgs: []string{"cancel"},
			flags:     map[string]string{"job_ids": "a, b ,c", "job_id": "ignored"},
			want:      map[string]any{"action": "cancel", "job_ids": []string{"a", "b", "c"}},
		},
		{
			name:      "no job id is a usage error",
			craftArgs: []string{"cancel"},
			flags:     map[string]string{},
			wantErr:   true,
		},
		{
			name:      "empty job_ids list is an error",
			craftArgs: []string{"cancel"},
			flags:     map[string]string{"job_ids": " , "},
			wantErr:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := craftCancelPayload(c.craftArgs, c.flags)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got payload %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("payload mismatch:\n got  %#v\n want %#v", got, c.want)
			}
		})
	}
}
