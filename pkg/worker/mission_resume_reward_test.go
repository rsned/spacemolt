package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestMissionsResumeCarriesRewardAndExpiry pins the reward and expiry budget
// across a resume.
//
// A mission picked off the board carries Rewards.Credits, so missionSelect
// derives a real `reward`. A mission RESUMED after a restart is rebuilt from
// get_active_missions, and the rebuild copied only id/template/type/title/
// chain_next — dropping Rewards and ExpiresInTicks, both of which
// ActiveMission actually carries. Consequences, all live-observed 2026-07-31
// after the mission-learn restart:
//
//   - mission_results.expected_reward recorded 0 for every resumed mission.
//     Eleven such rows collected 10,000 credits against a recorded
//     expectation of zero, skewing any profitability read of the fleet.
//   - the economics gate computes net = reward - costs, so a resumed mission
//     always looks like a pure loss; these only ran because the smuggling
//     XP-led branch accepts regardless of net.
//   - missionLogShortfall fires on earned < reward, which can never be true
//     at reward 0 — so exactly the missions with a broken expectation were
//     the ones that stayed silent.
//   - expiry_budget_ticks recorded 0, making the lateness question
//     unanswerable for the resumed half of the data.
func TestMissionsResumeCarriesRewardAndExpiry(t *testing.T) {
	active := serverapi.ActiveMission{
		MissionID: "held", Type: "delivery", Title: "Courier Run: Pirate Moonshine",
		ExpiresInTicks: 909,
		Rewards:        &serverapi.MissionRewards{Credits: 2000},
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "iron_ore", Required: 50, Current: 50, Completed: true, SystemID: "sol", TargetBase: "sol_station"},
		},
	}
	fc := &fakeClient{
		state:          missionState(true, 5000, 0),
		completeReward: 2000,
		raw: map[string][]byte{
			"missions":        boardJSON(t),
			"active_missions": activeJSON(t, active),
		},
	}
	store := &fakeMissionStore{}
	if err := Missions(context.Background(), missionDeps(fc, store, missionKB())); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %d", len(store.results))
	}
	if got := store.results[0].ExpectedReward; got != 2000 {
		t.Fatalf("resume must carry the active mission's reward (2000), got %v", got)
	}
	if got := store.results[0].ExpiryBudgetTicks; got != 909 {
		t.Fatalf("resume must carry the active mission's expiry budget (909), got %v", got)
	}
}
