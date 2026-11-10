package serverapi

// MissionPointer names a mission and, when it is tied to one station, where it
// is offered (openapi MissionPointer, v0.685.0). Pass MissionID to
// accept_mission while docked at BaseID.
type MissionPointer struct {
	MissionID  string `json:"mission_id"`
	Title      string `json:"title"`
	BaseID     string `json:"base_id,omitempty"`
	BaseName   string `json:"base_name,omitempty"`
	SystemID   string `json:"system_id,omitempty"`
	SystemName string `json:"system_name,omitempty"`
}

// MissionPrerequisite is a prerequisite mission the player has not completed,
// and NextStep, the earliest uncompleted mission in that prerequisite's chain
// (equal to Mission when every earlier step is done).
type MissionPrerequisite struct {
	Mission  MissionPointer `json:"mission"`
	NextStep MissionPointer `json:"next_step"`
}

// MissionUnlock is one mission that names a completed mission as a
// prerequisite (complete_mission / view_completed_mission "unlocks").
// Available means every requirement except location is met: dock at
// Mission.BaseID and accept. Available=false with no StillRequires means a
// non-mission requirement (skill, faction, credits) is unmet.
type MissionUnlock struct {
	Available     bool                  `json:"available"`
	Mission       MissionPointer        `json:"mission"`
	StillRequires []MissionPrerequisite `json:"still_requires,omitempty"`
}

// LockedMissionHint is a get_missions "locked_missions" entry: a mission only
// this station offers, partly unlocked by the player. The locked mission
// itself is not named; StillRequires is never empty.
type LockedMissionHint struct {
	StillRequires []MissionPrerequisite `json:"still_requires"`
}
