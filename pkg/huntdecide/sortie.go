package huntdecide

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// SortieFile is the name of the per-agent sortie state file inside the agent directory.
const SortieFile = "hunt-sortie.json"

// Sortie result values.
const (
	// ResultCompleted means the sortie finished normally.
	ResultCompleted = "completed"
	// ResultReturnedDry means the hunter returned out of ammunition.
	ResultReturnedDry = "returned_dry"
	// ResultReturnedDamaged means the hunter returned with a damaged hull.
	ResultReturnedDamaged = "returned_damaged"
	// ResultNoQuarry means no quarry was found.
	ResultNoQuarry = "no_quarry"
	// ResultBlockedUnarmed means the hunter was blocked for lack of weapons.
	ResultBlockedUnarmed = "blocked_unarmed"
	// ResultStranded means the hunter was stranded.
	ResultStranded = "stranded"
	// ResultDestroyed means the hunter's ship was destroyed.
	ResultDestroyed = "destroyed"
	// ResultInterrupted means the sortie was interrupted before it closed normally.
	ResultInterrupted = "interrupted"
)

// ResultFailed reports whether result is one of the failed outcomes.
func ResultFailed(result string) bool {
	switch result {
	case ResultBlockedUnarmed, ResultStranded, ResultDestroyed:
		return true
	}
	return false
}

// Sortie is a hunter's current sortie, carried between worker passes.
type Sortie struct {
	ID           string        `json:"id"`
	Seq          int           `json:"seq"`
	StartedAt    time.Time     `json:"started_at"`
	StartTick    int64         `json:"start_tick"`
	JobKind      string        `json:"job_kind"`
	JobID        string        `json:"job_id"`
	Grounds      []GroundVisit `json:"grounds"`
	Fights       int           `json:"fights"`
	Kills        int           `json:"kills"`
	ReloadsSpent int           `json:"reloads_spent"`
	// PendingResult is set when heading home: the result to close with on docking.
	PendingResult string `json:"pending_result"`
}

// NewSortie starts a sortie for agentID at now.
func NewSortie(agentID string, now time.Time) *Sortie {
	return &Sortie{
		ID:        agentID + "-" + now.UTC().Format("20060102T150405Z"),
		StartedAt: now,
	}
}

// NextSeq increments the decision sequence number and returns it; the first call returns 1.
func (s *Sortie) NextSeq() int {
	s.Seq++
	return s.Seq
}

// Visited returns the POI ids of the grounds visited so far.
func (s *Sortie) Visited() []string {
	out := make([]string, 0, len(s.Grounds))
	for _, g := range s.Grounds {
		out = append(out, g.POI)
	}
	return out
}

// LoadSortie reads the sortie file in agentDir. A missing file returns nil, nil;
// an undecodable file returns nil and an error.
func LoadSortie(agentDir string) (*Sortie, error) {
	data, err := os.ReadFile(filepath.Join(agentDir, SortieFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sortie: %w", err)
	}
	var s Sortie
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode sortie: %w", err)
	}
	return &s, nil
}

// Save writes the sortie to agentDir atomically (temp file plus rename), creating
// agentDir if needed.
func (s *Sortie) Save(agentDir string) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode sortie: %w", err)
	}
	if err = os.MkdirAll(agentDir, 0o755); err != nil {
		return fmt.Errorf("create sortie dir: %w", err)
	}
	tmp, err := os.CreateTemp(agentDir, SortieFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp sortie: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write sortie: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close sortie: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(agentDir, SortieFile)); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename sortie: %w", err)
	}
	return nil
}

// ClearSortie removes the sortie file; a missing file is not an error.
func ClearSortie(agentDir string) error {
	err := os.Remove(filepath.Join(agentDir, SortieFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove sortie: %w", err)
	}
	return nil
}
