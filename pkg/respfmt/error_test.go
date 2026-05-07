package respfmt

import (
	"errors"
	"testing"
)

func TestError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		command string
		want    string
	}{
		{"nil err", nil, "anything", ""},
		{"plain pass-through", errors.New("boom"), "buy", "Error: boom"},
		{
			"mine depleted override",
			errors.New("resource depleted"),
			"mine",
			"Ore depleted.",
		},
		{
			"override only matches its command",
			errors.New("resource depleted"),
			"buy",
			"Error: resource depleted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Error(tt.err, tt.command); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}
