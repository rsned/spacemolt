package worker

import (
	"encoding/json"
	"os"
	"strings"
)

// DefaultAvoidSystemsPath is the operator's list of systems workers keep out
// of (a roaming Molt Leviathan made Izar Belt a kill zone for miners on
// 2026-11-13). It is re-read on every check, so an edit takes effect without
// restarting any worker.
const DefaultAvoidSystemsPath = "data/overmind/avoid-systems.json"

// avoidSystemsFile is {"systems": {"<system_id>": "<reason>"}}.
type avoidSystemsFile struct {
	Systems map[string]string `json:"systems"`
}

// avoidedSystem reports whether systemID is on the avoid list. A missing or
// unreadable file avoids nothing.
func (d *WorkerDispatch) avoidedSystem(systemID string) bool {
	path := d.AvoidSystemsPath
	if path == "" {
		path = DefaultAvoidSystemsPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var f avoidSystemsFile
	if json.Unmarshal(data, &f) != nil {
		return false
	}
	_, ok := f.Systems[strings.ToLower(systemID)]
	return ok
}
