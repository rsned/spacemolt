package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// surveyAnomalyDirection matches the directional tail of a survey anomaly hint,
// e.g. "... toward Furud (3 jumps)" or "... towards Nova Terra (1 jump)". Group 1
// is the (possibly multi-word) target system name, group 2 the jump count.
var surveyAnomalyDirection = regexp.MustCompile(`(?i)towards?\s+(.+?)\s+\((\d+)\s+jumps?\)`)

// surveyAnomalyDetails is the structured payload stored in Anomaly.Details for a
// captured survey hint.
type surveyAnomalyDetails struct {
	Raw          string `json:"raw"`
	TargetSystem string `json:"target_system,omitempty"`
	Jumps        int    `json:"jumps,omitempty"`
	InSystem     bool   `json:"in_system"`
}

// ParseSurveyAnomaly turns a survey_system AnomalyHint into an Anomaly record.
// It returns ok=false for an empty hint. When the hint points toward another
// system ("... toward X (N jumps)") the target system and jump count are parsed
// into Details; an "in this system" hint sets InSystem=true. The raw hint is
// always preserved as the Description and in Details.raw.
func ParseSurveyAnomaly(hint, systemID, detectedBy string, tick int64) (Anomaly, bool) {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return Anomaly{}, false
	}

	d := surveyAnomalyDetails{Raw: hint}
	if m := surveyAnomalyDirection.FindStringSubmatch(hint); m != nil {
		d.TargetSystem = strings.TrimSpace(m[1])
		d.Jumps, _ = strconv.Atoi(m[2])
	} else if strings.Contains(strings.ToLower(hint), "in this system") {
		d.InSystem = true
	}

	details, _ := json.Marshal(d)

	return Anomaly{
		Type:            "spatial_anomaly",
		Severity:        "opportunity",
		SystemID:        systemID,
		Description:     hint,
		Details:         string(details),
		DetectedBy:      detectedBy,
		LastUpdatedTick: tick,
	}, true
}

// CaptureSurveyAnomaly parses a survey anomaly hint and persists it, skipping
// the write when an active anomaly with the same type and description already
// exists for the system (so repeat surveys don't pile up duplicates). It returns
// (true, nil) only when a new row was recorded; an empty hint is a silent no-op.
func CaptureSurveyAnomaly(ctx context.Context, kb Base, hint, systemID, detectedBy string, tick int64) (bool, error) {
	anomaly, ok := ParseSurveyAnomaly(hint, systemID, detectedBy, tick)
	if !ok {
		return false, nil
	}

	existing, err := kb.GetActiveAnomalies(ctx, systemID)
	if err != nil {
		return false, fmt.Errorf("check existing anomalies: %w", err)
	}
	for _, e := range existing {
		if e.Type == anomaly.Type && e.Description == anomaly.Description {
			return false, nil
		}
	}

	if err := kb.RecordAnomaly(ctx, anomaly); err != nil {
		return false, fmt.Errorf("record survey anomaly: %w", err)
	}
	return true, nil
}
