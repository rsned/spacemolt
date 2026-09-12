package worker

import (
	"path/filepath"
	"testing"
)

// Every scheduled command in the seeded roles.yaml must be one the dispatcher
// actually implements.
//
// roles.yaml validation only rejects an EMPTY command, so a typo or a command
// that was never wired into the dispatch switch loads cleanly and then fails
// once per agent per interval, forever, as a log line nobody reads. That is
// how a scheduled `craft refine_steel 500` would have failed silently across
// ~50 resident marketbots when it was added on 2026-09-11.
//
// Only the FIRST token is checked: a schedule entry is a command LINE and may
// carry arguments and $TOKEN$s, which are resolved at run time against live
// state.
func TestRolesYAMLCommandsAreAllDispatchable(t *testing.T) {
	roles, err := LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	d := &WorkerDispatch{}

	checked := 0
	for name, role := range roles {
		for _, se := range role.Schedule {
			tokens := SplitArgs(se.Command)
			if len(tokens) == 0 {
				t.Errorf("role %q: empty scheduled command", name)
				continue
			}
			checked++
			if !d.Supports(tokens[0]) {
				t.Errorf("role %q schedules %q, but the dispatcher has no %q command",
					name, se.Command, tokens[0])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no scheduled commands found — the loader or the fixture changed shape")
	}
}
