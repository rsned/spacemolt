package main

import "testing"

func TestParseBuildArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		target  string
		qty     int
		jsonOut bool
		wantErr bool
	}{
		{name: "target only defaults qty 1", args: []string{"sensor_array"}, target: "sensor_array", qty: 1},
		{name: "explicit qty", args: []string{"sensor_array", "25"}, target: "sensor_array", qty: 25},
		{name: "json flag", args: []string{"sensor_array", "2", "--json"}, target: "sensor_array", qty: 2, jsonOut: true},
		{name: "max-hand-ticks accepted", args: []string{"sensor_array", "--max-hand-ticks=50"}, target: "sensor_array", qty: 1},
		{name: "no args", args: nil, wantErr: true},
		{name: "zero qty", args: []string{"x", "0"}, wantErr: true},
		{name: "bad qty", args: []string{"x", "abc"}, wantErr: true},
		// A silently-ignored typo would leave the plan on the default time
		// budget while the operator believes they set one.
		{name: "typo'd max-hand-ticks rejected", args: []string{"x", "--max-hand-tick=50"}, wantErr: true},
		{name: "unknown flag rejected", args: []string{"x", "--jsonn"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, qty, jsonOut, err := parseBuildArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if target != tt.target || qty != tt.qty || jsonOut != tt.jsonOut {
				t.Errorf("got (%q,%d,%v), want (%q,%d,%v)", target, qty, jsonOut, tt.target, tt.qty, tt.jsonOut)
			}
		})
	}
}
