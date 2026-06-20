package checkpoint

import "fmt"

// Disposition is the reconciliation outcome.
type Disposition int

const (
	// Resume means saved intent can be safely continued.
	Resume Disposition = iota
	// Diverged means live state contradicts the checkpoint; re-plan instead.
	Diverged
)

// String renders a Disposition for logs.
func (d Disposition) String() string {
	if d == Diverged {
		return "diverged"
	}
	return "resume"
}

// Reconciliation is the result of comparing a checkpoint against live state.
type Reconciliation struct {
	Disposition Disposition
	Reasons     []string
}

// Reconcile compares saved checkpoint state to freshly-fetched live state.
// A zero-value saved state (fresh worker, no checkpoint) always returns Resume.
// creditDropFraction is the fraction of saved credits whose loss flags divergence.
func Reconcile(saved, live KnownState, creditDropFraction float64) Reconciliation {
	if (saved == KnownState{}) {
		return Reconciliation{Disposition: Resume}
	}
	var reasons []string
	if saved.System != live.System {
		reasons = append(reasons, fmt.Sprintf("system changed %q->%q", saved.System, live.System))
	}
	if saved.Docked != live.Docked {
		reasons = append(reasons, fmt.Sprintf("docked changed %v->%v", saved.Docked, live.Docked))
	}
	if saved.Credits > 0 && live.Credits < saved.Credits*(1-creditDropFraction) {
		reasons = append(reasons, fmt.Sprintf("credits dropped %.0f->%.0f", saved.Credits, live.Credits))
	}
	if len(reasons) > 0 {
		return Reconciliation{Disposition: Diverged, Reasons: reasons}
	}
	return Reconciliation{Disposition: Resume}
}
