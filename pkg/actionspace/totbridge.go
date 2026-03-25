package actionspace

// ActionOption matches the tot.ActionOption type for bridging to the ToT pipeline.
// This avoids an import cycle between actionspace and tot.
type ActionOption struct {
	Action      string   `json:"action"`
	Description string   `json:"description"`
	Targets     []string `json:"targets,omitempty"`
}

// ToActionOptions converts valid actions to the ActionOption format
// used by the ToT pipeline. Only includes actions that passed all preconditions.
func (as ActionSpace) ToActionOptions() []ActionOption {
	var opts []ActionOption
	for _, r := range as.Actions {
		if !r.Valid {
			continue
		}
		opts = append(opts, ActionOption{
			Action:      r.Action.Name,
			Description: r.Action.Summary,
			Targets:     r.Targets,
		})
	}
	return opts
}
