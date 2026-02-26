package agent

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestIsMarketAnalysisFresh(t *testing.T) {
	tests := []struct {
		name       string
		capturedAt time.Time
		want       bool
	}{
		{
			name:       "just_captured",
			capturedAt: time.Now(),
			want:       true,
		},
		{
			name:       "five_minutes_ago",
			capturedAt: time.Now().Add(-5 * time.Minute),
			want:       true,
		},
		{
			name:       "at_threshold",
			capturedAt: time.Now().Add(-MarketAnalysisFreshnessThreshold),
			want:       false,
		},
		{
			name:       "well_past_threshold",
			capturedAt: time.Now().Add(-2 * MarketAnalysisFreshnessThreshold),
			want:       false,
		},
		{
			name:       "zero_time",
			capturedAt: time.Time{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMarketAnalysisFresh(tt.capturedAt)
			if got != tt.want {
				t.Errorf("isMarketAnalysisFresh(%v) = %v, want %v", tt.capturedAt, got, tt.want)
			}
		})
	}
}

func TestIsMarketDataFresh(t *testing.T) {
	tests := []struct {
		name       string
		capturedAt time.Time
		want       bool
	}{
		{
			name:       "just_captured",
			capturedAt: time.Now(),
			want:       true,
		},
		{
			name:       "three_minutes_ago",
			capturedAt: time.Now().Add(-3 * time.Minute),
			want:       true,
		},
		{
			name:       "at_threshold",
			capturedAt: time.Now().Add(-MarketFreshnessThreshold),
			want:       false,
		},
		{
			name:       "well_past_threshold",
			capturedAt: time.Now().Add(-2 * MarketFreshnessThreshold),
			want:       false,
		},
		{
			name:       "zero_time",
			capturedAt: time.Time{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMarketDataFresh(tt.capturedAt)
			if got != tt.want {
				t.Errorf("isMarketDataFresh(%v) = %v, want %v", tt.capturedAt, got, tt.want)
			}
		})
	}
}

func TestShouldCaptureMarket(t *testing.T) {
	tests := []struct {
		name   string
		state  *game.State
		status Status
		want   bool
	}{
		{
			name:   "idle_agent",
			state:  &game.State{},
			status: Status{State: AgentStateIdle},
			want:   true,
		},
		{
			name:   "docked_ship",
			state:  &game.State{Doc: true},
			status: Status{State: AgentStateActing},
			want:   true,
		},
		{
			name:   "neither_idle_nor_docked",
			state:  &game.State{Doc: false},
			status: Status{State: AgentStateDeciding},
			want:   false,
		},
		{
			name:   "error_state_not_docked",
			state:  &game.State{Doc: false},
			status: Status{State: AgentStateError},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldCaptureMarket(tt.state, tt.status)
			if got != tt.want {
				t.Errorf("ShouldCaptureMarket() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarketFreshnessThresholdValues(t *testing.T) {
	// Verify the constants are set to expected values
	if MarketFreshnessThreshold != 360*time.Second {
		t.Errorf("MarketFreshnessThreshold = %v, want %v", MarketFreshnessThreshold, 360*time.Second)
	}
	if MarketAnalysisFreshnessThreshold != 720*time.Second {
		t.Errorf("MarketAnalysisFreshnessThreshold = %v, want %v", MarketAnalysisFreshnessThreshold, 720*time.Second)
	}
}
