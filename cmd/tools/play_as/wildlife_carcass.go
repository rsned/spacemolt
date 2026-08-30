package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// wreckTypeCreature is the get_wrecks type of a wildlife carcass. A creature
// wreck carries victim_name but an EMPTY ship_class, so it cannot name its own
// species (pkg/worker/hunt.go documents the same shape).
const wreckTypeCreature = "creature"

// capturedCarcasses remembers the wreck ids already recorded this session so
// that re-running `wrecks` before `loot` does not count one kill twice.
var capturedCarcasses = map[string]bool{}

// captureCarcasses records every creature wreck in the client's last get_wrecks
// reply as a wildlife kill with its drop roll, mirroring what the worker hunt
// loop does at kill time (huntLootCarcass). play_as is the only place hand-flown
// kills happen, and until 2026-08-30 it read the carcass and threw the drops
// away — wildlife_kills and wildlife_kill_drops had never held a row.
//
// Species resolution: the get_nearby entry that named the creature wins (it is
// still in the raw cache unless nearby was re-run after the kill); otherwise the
// species is inferred from victim_name. Contents are recorded BEFORE looting so
// the drop table is the server's roll, not a function of hold space.
//
// Returns the number of carcasses newly recorded. Never fails the caller: a
// capture problem is printed and the wreck is left uncaptured for a retry.
func captureCarcasses(ctx context.Context, client game.GameClient, kb knowledge.Base, agentID string, seen map[string]bool, out io.Writer) int {
	if kb == nil || client == nil {
		return 0
	}
	raw := client.GetRawJSON("wrecks")
	if len(raw) == 0 {
		return 0
	}
	var resp serverapi.GetWrecksResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(out, "  (wrecks not parsed for wildlife capture: %v)\n", err) //nolint:errcheck
		return 0
	}

	var nearby serverapi.GetNearbyResponse
	if rawNearby := client.GetRawJSON("nearby"); len(rawNearby) > 0 {
		_ = json.Unmarshal(rawNearby, &nearby)
	}

	systemID, tick, battleID := "", int64(0), ""
	if st := client.GetState(); st != nil {
		systemID, tick, battleID = st.System.ID, st.CurrentTick, st.LastBattleID
	}

	n := 0
	for i := range resp.Wrecks {
		w := &resp.Wrecks[i]
		if w.Type != wreckTypeCreature || w.VictimID == "" || seen[w.ID] {
			continue
		}
		creature := serverapi.NearbyCreature{CreatureID: w.VictimID, Name: w.VictimName}
		inferred := true
		for _, c := range nearby.Creatures {
			if c.CreatureID == w.VictimID {
				creature, inferred = c, false
				break
			}
		}
		if creature.Species == "" {
			creature.Species = speciesFromVictimName(w.VictimName)
		}
		if w.SystemID != "" {
			systemID = w.SystemID
		}
		if err := knowledge.CaptureWildlifeCarcass(ctx, kb, w, creature, systemID, battleID, agentID, tick, 0, 0, 0); err != nil {
			fmt.Fprintf(out, "  (kill of %s not recorded: %v)\n", w.VictimName, err) //nolint:errcheck
			continue
		}
		seen[w.ID] = true
		n++
		note := ""
		if inferred {
			note = " (species inferred from name)"
		}
		fmt.Fprintf(out, "  Wildlife kill recorded: %s [%s], %d drop stack(s)%s\n", w.VictimName, creature.Species, len(w.Cargo), note) //nolint:errcheck
	}
	return n
}

// speciesFromVictimName turns a carcass's display name into the species key
// get_nearby would have reported: "Belt-Grazer" -> belt_grazer,
// "Rainbow Leviathan" -> rainbow_leviathan. It is the fallback for a creature
// no longer in the nearby cache.
func speciesFromVictimName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.NewReplacer(" ", "_", "-", "_", "'", "").Replace(name)
	return name
}
