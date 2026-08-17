package main

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// stateWithSurveyScanner returns a state whose fit resolves survey_scanner_i the
// way a get_ship reply populates it: Ship.Modules holds instance ids, and
// ModuleDefinitions maps each id to its type_id. The ids are the real ones
// observed on the wire, so the shape under test matches production exactly.
func stateWithSurveyScanner() *game.State {
	st := &game.State{}
	st.Ship.ClassID = "floor_price"
	st.Ship.Modules = []string{"c1bab746e71529711f9d8e34fef8a061"}
	st.ModuleDefinitions = map[string]game.ModuleDefinition{
		"c1bab746e71529711f9d8e34fef8a061": {
			ID:     "c1bab746e71529711f9d8e34fef8a061",
			Name:   "Survey Scanner I",
			Type:   "utility",
			TypeID: "survey_scanner_i",
		},
	}
	return st
}

// stateWithoutSurveyScanner returns a state carrying an unrelated module, which
// is also what a pre-install state looks like: the scanner is fitted on the
// server but absent from the client's copy of the fit.
func stateWithoutSurveyScanner() *game.State {
	st := &game.State{}
	st.Ship.ClassID = "floor_price"
	st.Ship.Modules = []string{"1411dea797ed3b9093de1f81d82e90d9"}
	st.ModuleDefinitions = map[string]game.ModuleDefinition{
		"1411dea797ed3b9093de1f81d82e90d9": {
			ID:     "1411dea797ed3b9093de1f81d82e90d9",
			Name:   "Cargo Expander III",
			Type:   "utility",
			TypeID: "cargo_expander_iii",
		},
	}
	return st
}

// resetSurveyScannerCache clears the package-level cache before and after a
// test. The cache is global, so a test that left a stale value behind would
// silently decide the outcome of the next one.
func resetSurveyScannerCache(t *testing.T) {
	t.Helper()
	invalidateSurveyScannerCache()
	t.Cleanup(invalidateSurveyScannerCache)
}

// TestCheckForSurveyScanner_FindsInstalledModule is the baseline: a fit that
// contains the scanner must resolve through ModuleDefinitions to true.
func TestCheckForSurveyScanner_FindsInstalledModule(t *testing.T) {
	resetSurveyScannerCache(t)

	if !checkForSurveyScanner(stateWithSurveyScanner()) {
		t.Fatal("checkForSurveyScanner: got false for a fit containing survey_scanner_i, want true")
	}
}

// TestCheckForSurveyScanner_NegativeIsNotCached guards the reported bug: a "no"
// used to be cached permanently, so a scanner installed mid-session could never
// be seen again — only switch_ship cleared it. A later call with an up-to-date
// state must answer true without any explicit invalidation.
func TestCheckForSurveyScanner_NegativeIsNotCached(t *testing.T) {
	resetSurveyScannerCache(t)

	if checkForSurveyScanner(stateWithoutSurveyScanner()) {
		t.Fatal("checkForSurveyScanner: got true for a fit with no scanner, want false")
	}

	// Same session, no invalidation — the module has since been installed and
	// the state refreshed.
	if !checkForSurveyScanner(stateWithSurveyScanner()) {
		t.Fatal("checkForSurveyScanner: negative was cached; got false after the scanner appeared in state, want true")
	}
}

// TestCheckForSurveyScanner_PositiveIsCached keeps the caching that is actually
// safe: once a scanner is confirmed, the answer is reused without re-walking the
// fit. Only a ship change (switch_ship, install, uninstall) invalidates it.
func TestCheckForSurveyScanner_PositiveIsCached(t *testing.T) {
	resetSurveyScannerCache(t)

	if !checkForSurveyScanner(stateWithSurveyScanner()) {
		t.Fatal("checkForSurveyScanner: got false for a fit containing survey_scanner_i, want true")
	}
	if !checkForSurveyScanner(&game.State{}) {
		t.Fatal("checkForSurveyScanner: positive was not cached; got false for an empty state, want the cached true")
	}
}

// TestCheckForSurveyScanner_NilState guards the nil deref that a second call
// would otherwise risk: GetState can return nil, and the hull-capability
// fallback reads state.Ship.ClassID.
func TestCheckForSurveyScanner_NilState(t *testing.T) {
	resetSurveyScannerCache(t)

	if checkForSurveyScanner(nil) {
		t.Fatal("checkForSurveyScanner(nil): got true, want false")
	}
}

// stubSurveyClient is the minimum of game.GameClient that surveySystem touches
// before it either refuses or issues the survey. GetShip flips the state from
// scanner-less to scanner-fitted, standing in for the server telling us the real
// fit; SurveySystem records that the gate was passed and fails immediately so
// the test never reaches the survey loop's sleeps.
type stubSurveyClient struct {
	game.GameClient

	refreshed  bool // GetShip was called
	surveyed   bool // SurveySystem was called (i.e. the gate opened)
	shipOnWire *game.State
	getShipErr error
}

func (s *stubSurveyClient) GetState() *game.State {
	if s.refreshed {
		return s.shipOnWire
	}
	return stateWithoutSurveyScanner()
}

func (s *stubSurveyClient) GetShip(context.Context) error {
	if s.getShipErr != nil {
		return s.getShipErr
	}
	s.refreshed = true
	return nil
}

func (s *stubSurveyClient) SurveySystem(context.Context) error {
	s.surveyed = true
	return context.Canceled // stop surveySystem before its first sleep
}

// TestSurveySystem_RefreshesStaleShipState covers the reported symptom end to
// end: install_mod's reply carries only module_id, so the client's fit is stale
// and the gate would refuse a ship that genuinely has the scanner. surveySystem
// must re-read the ship before refusing, and then proceed.
func TestSurveySystem_RefreshesStaleShipState(t *testing.T) {
	resetSurveyScannerCache(t)

	client := &stubSurveyClient{shipOnWire: stateWithSurveyScanner()}
	surveySystem(client, context.Background(), formatRaw)

	if !client.refreshed {
		t.Error("surveySystem: did not call GetShip before refusing on a stale fit")
	}
	if !client.surveyed {
		t.Error("surveySystem: refused to survey a ship whose refreshed fit contains survey_scanner_i")
	}
}

// TestSurveySystem_RefusesWhenRefreshConfirmsNoScanner proves the refresh does
// not turn the gate into a no-op: a ship that really has no scanner is still
// refused, and no survey is attempted.
func TestSurveySystem_RefusesWhenRefreshConfirmsNoScanner(t *testing.T) {
	resetSurveyScannerCache(t)

	client := &stubSurveyClient{shipOnWire: stateWithoutSurveyScanner()}
	surveySystem(client, context.Background(), formatRaw)

	if !client.refreshed {
		t.Error("surveySystem: did not call GetShip before refusing")
	}
	if client.surveyed {
		t.Error("surveySystem: surveyed with no scanner installed")
	}
}

// TestSurveySystem_RefusesWhenRefreshFails keeps a failed refresh from crashing
// or falsely passing: if get_ship errors, the pre-existing state stands and the
// command refuses.
func TestSurveySystem_RefusesWhenRefreshFails(t *testing.T) {
	resetSurveyScannerCache(t)

	client := &stubSurveyClient{
		shipOnWire: stateWithSurveyScanner(),
		getShipErr: context.DeadlineExceeded,
	}
	surveySystem(client, context.Background(), formatRaw)

	if client.surveyed {
		t.Error("surveySystem: surveyed after a failed ship refresh left a scanner-less state")
	}
}
