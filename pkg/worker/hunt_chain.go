package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// huntChainFile is the per-agent record of the mission chains this agent has
// EARNED the right to continue: it remembers, across passes and across worker
// restarts, that a completed mission named a follow-on.
//
// It has to be on disk because the two halves of the fact happen in different
// passes. The server names the continuation exactly once, in the reply to the
// complete_mission that earned it, and the mission it unlocks appears on a
// board some later pass reads. Nothing in memory survives the gap: a worker
// process outlives a pass, but not a restart, and the fleet restarts on every
// deploy.
//
// The shape follows freight-held.json next door (freight_persist.go) rather
// than inventing a second persistence idiom: same directory, same atomic
// tmp+rename write, same "a missing file is not an error" contract.
const huntChainFile = "hunt-chain.json"

// huntChainPath is <agentsDir>/<agentID>/hunt-chain.json, with agentsDir
// defaulting to DefaultAgentsDir exactly as UsernameFor does.
func huntChainPath(agentsDir, agentID string) string {
	if agentsDir == "" {
		agentsDir = DefaultAgentsDir
	}
	return filepath.Join(agentsDir, agentID, huntChainFile)
}

// huntChainRecord is one earned continuation: which mission unlocked it, and
// when. CompletedAt is not decoration — it is the per-record proof that the
// predecessor was COMPLETED rather than merely seen or accepted, which is the
// whole condition the difficulty waiver rests on. A record without it is not
// credited (huntRecordedContinuations), so neither a truncated write nor a
// hand-edited file can forge an exemption out of a half-fact.
type huntChainRecord struct {
	// Predecessor is the board/template id of the completed mission, which is
	// what the logs and the curated tables key on.
	Predecessor string `json:"predecessor"`
	// ActiveID is the hex instance id the completion reply reported, kept so a
	// recorded exemption can be traced back to one specific completion.
	ActiveID string `json:"active_id,omitempty"`
	Title    string `json:"title,omitempty"`
	// CompletedAt is when this worker recorded the completion (RFC3339, UTC).
	CompletedAt string `json:"completed_at"`
}

// loadHuntChain reads the recorded continuations, keyed by the mission id each
// one unlocks. A missing file is not an error (a fresh agent, or one that has
// never finished a chain step): it returns (nil, nil).
func loadHuntChain(path string) (map[string]huntChainRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("hunt-chain read: %w", err)
	}
	var recs map[string]huntChainRecord
	if err := json.Unmarshal(raw, &recs); err != nil {
		return nil, fmt.Errorf("hunt-chain decode: %w", err)
	}
	return recs, nil
}

// saveHuntChain writes the record set atomically (tmp + rename), creating the
// agent directory if needed.
func saveHuntChain(path string, recs map[string]huntChainRecord) error {
	if recs == nil {
		recs = map[string]huntChainRecord{}
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return fmt.Errorf("hunt-chain marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("hunt-chain mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("hunt-chain write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("hunt-chain replace: %w", err)
	}
	return nil
}

// huntRecordChainNext reads the reply to the complete_mission this pass just
// issued and records any chain continuation it names.
//
// This frame is the PRIMARY evidence for the difficulty waiver, and it is
// better evidence than any list could be. A captured completion of
// first_hunt_belt_grazers replies with chain_next "cracking_the_shell"
// alongside the mission_id, the title and the credits — so the agent completed
// the mission AND the server named the follow-on in one correlated reply.
// There is no container to discriminate, no shape to guess, and the
// "completed, not merely accepted" rule holds by construction: this frame
// cannot exist without a completion.
//
// The reply is action_result-WRAPPED — the body nests under "result" — which is
// a recorded defect class in this repo: a caller that reads the top-level keys
// decodes every field as its zero value and fails as an empty string rather
// than as an error (craft_node.go's unwrapActionResult, and shipping before
// it). Hence the unwrap.
//
// Two things one capture does not settle, so neither is assumed: whether a
// terminal chain step omits chain_next or sends it empty (both are treated as
// "no continuation", never as an error), and whether the completed-missions
// list carries the field at all (that path stays, as a secondary source).
//
// Every failure here is logged and swallowed. The pass is over and the mission
// is paid; the only cost of losing this frame is that the continuation stays
// gated, which is the direction this gate is supposed to fail.
func huntRecordChainNext(deps HuntDeps, out io.Writer, job huntJob) {
	raw := deps.Client.GetRawJSON("complete_mission")
	if len(raw) == 0 {
		fmt.Fprintf(out, "hunt: no complete_mission payload after %s; a chain continuation, if any, goes unrecorded\n", job.boardID) //nolint:errcheck
		return
	}
	var res serverapi.CompleteMissionResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &res); err != nil {
		fmt.Fprintf(out, "hunt: parse complete_mission reply: %v; a chain continuation, if any, goes unrecorded\n", err) //nolint:errcheck
		return
	}
	// The raw store keeps the LAST frame under this key, so a completion that
	// never landed leaves an earlier pass's reply sitting there. Crediting it
	// would record a continuation for a mission this pass did not finish —
	// missionCreditsEarned guards the same key the same way.
	if res.MissionID != job.activeID {
		fmt.Fprintf(out, "hunt: complete_mission reply is for %s, not %s; not counting it as chain evidence\n", res.MissionID, job.activeID) //nolint:errcheck
		return
	}
	if res.ChainNext == "" {
		fmt.Fprintf(out, "hunt: %s completed; the reply names no continuation\n", job.boardID) //nolint:errcheck
		return
	}
	if deps.AgentID == "" {
		fmt.Fprintf(out, "hunt: %s unlocks %s, but this pass has no agent id to record it under\n", job.boardID, res.ChainNext) //nolint:errcheck
		return
	}

	path := huntChainPath(deps.AgentsDir, deps.AgentID)
	recs, err := loadHuntChain(path)
	if err != nil {
		// An unreadable file has already lost whatever it held — nothing in it
		// can be credited either. Writing a fresh one at least preserves the
		// continuation just earned, rather than losing that too.
		fmt.Fprintf(out, "hunt: read %s: %v; starting a fresh chain record\n", path, err) //nolint:errcheck
		recs = nil
	}
	if recs == nil {
		recs = map[string]huntChainRecord{}
	}
	recs[res.ChainNext] = huntChainRecord{
		Predecessor: job.boardID,
		ActiveID:    res.MissionID,
		Title:       res.Title,
		CompletedAt: rfc(huntNow(deps)),
	}
	if err := saveHuntChain(path, recs); err != nil {
		fmt.Fprintf(out, "hunt: persist chain continuation: %v; %s stays gated\n", err, res.ChainNext) //nolint:errcheck
		return
	}
	fmt.Fprintf(out, "hunt: recorded chain continuation %s, earned by completing %s\n", res.ChainNext, job.boardID) //nolint:errcheck
}

// huntRecordedContinuations returns the continuations this agent has earned
// according to its own on-disk record: continuation mission id -> the completed
// predecessor that unlocked it.
//
// Every failure path returns an EMPTY map. No agent id, no file, an unreadable
// file, a record missing its completion marker — all of them mean the plain
// difficulty cap applies. The waiver is the exception, so absent evidence must
// read as "not earned" and never as "unknown, allow it".
func huntRecordedContinuations(deps HuntDeps, out io.Writer) map[string]string {
	earned := map[string]string{}
	if deps.AgentID == "" {
		return earned
	}
	path := huntChainPath(deps.AgentsDir, deps.AgentID)
	recs, err := loadHuntChain(path)
	if err != nil {
		fmt.Fprintf(out, "hunt: read %s: %v; recorded chain continuations stay gated\n", path, err) //nolint:errcheck
		return earned
	}
	for next, rec := range recs {
		if next == "" || rec.Predecessor == "" {
			continue
		}
		if rec.CompletedAt == "" {
			// Same rule as the list path's completion_time: no completion
			// marker, no credit.
			fmt.Fprintf(out, "hunt: ignoring recorded %s as chain evidence: no completed_at\n", next) //nolint:errcheck
			continue
		}
		earned[next] = rec.Predecessor
	}
	return earned
}
