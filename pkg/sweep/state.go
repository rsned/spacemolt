package sweep

// State is one ship's sweep progress. Task 4 extends it and adds persistence.
type State struct {
	Ring        float64 `json:"ring"`
	ProgressDeg float64 `json:"progress_deg"`
	SweptDeg    float64 `json:"swept_deg"`
}
