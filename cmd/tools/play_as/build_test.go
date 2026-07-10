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
		{name: "no args", args: nil, wantErr: true},
		{name: "zero qty", args: []string{"x", "0"}, wantErr: true},
		{name: "bad qty", args: []string{"x", "abc"}, wantErr: true},
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
