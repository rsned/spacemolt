package strategy

// SkillCheckpoint captures the state of a background skill that was interrupted,
// allowing it to resume from where it left off.
type SkillCheckpoint struct {
	// SkillName is the name of the interrupted skill (e.g. "mine").
	SkillName string

	// CurrentStep is the step ID where execution was interrupted.
	CurrentStep string

	// StepState holds skill-specific state at the time of interruption.
	StepState map[string]any

	// Interrupted is true if the skill was interrupted (vs. completing naturally).
	Interrupted bool

	// CleanupDone is true if the interrupt cleanup sequence completed successfully.
	CleanupDone bool
}

// IsEmpty returns true if no checkpoint data is stored.
func (c *SkillCheckpoint) IsEmpty() bool {
	return c.SkillName == "" && c.CurrentStep == ""
}

// Clear resets the checkpoint to empty state.
func (c *SkillCheckpoint) Clear() {
	c.SkillName = ""
	c.CurrentStep = ""
	c.StepState = nil
	c.Interrupted = false
	c.CleanupDone = false
}
