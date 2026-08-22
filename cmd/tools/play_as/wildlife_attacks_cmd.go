package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/rsned/spacemolt/pkg/battlereplay"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// captureWildlifeAttacksCmd records what a creature shot with, read from a
// battle log after the fight.
//
// The battle id is REQUIRED here, unlike the worker command of the same name.
// The worker's no-arg form drains a queue derived from wildlife_kills -- battles
// in which the agent KILLED something -- which is exactly the wrong shape for
// hand-driven testing, where the interesting fight is often the one the creature
// won. Naming the battle bypasses the queue and works win or lose.
//
// The id is only obtainable at battle start; it cannot be recovered afterwards.
func captureWildlifeAttacksCmd(ctx context.Context, out io.Writer, kb knowledge.Base, client game.GameClient, args []string) error {
	if len(args) == 0 || args[0] == "" {
		return fmt.Errorf("usage: capture_wildlife_attacks <battle_id>")
	}
	if kb == nil {
		fmt.Fprintln(out, "capture_wildlife_attacks: no knowledge base configured (use --db-path)") //nolint:errcheck

		return nil
	}
	battleID := args[0]
	n, err := battlereplay.CaptureWildlifeAttacks(ctx, kb, client, battleID, nil, nil)
	if err != nil {
		return err
	}
	if n == 0 {
		// Zero is ambiguous and silently so, which is worth spelling out: the
		// capture files nothing both when the battle had no creatures at all
		// and when it had creatures whose species could not be named. Species
		// resolution falls back to a display-name index that only ever matches
		// a species the field guide ALREADY holds, so a first encounter with a
		// new creature lands here even though the damage is right there in the
		// log.
		fmt.Fprintf(out, "capture_wildlife_attacks: battle %s filed 0 rows.\n", battleID)              //nolint:errcheck
		fmt.Fprintln(out, "  Either the battle had no creature participants (a player/pirate fight),") //nolint:errcheck
		fmt.Fprintln(out, "  or its creatures are not in the field guide yet, so the species could")   //nolint:errcheck
		fmt.Fprintln(out, "  not be resolved. Run get_battle_log <battle_id> to see which.")           //nolint:errcheck

		return nil
	}
	fmt.Fprintf(out, "capture_wildlife_attacks: battle %s -> %d row(s)\n", battleID, n) //nolint:errcheck

	return nil
}

// getBattleLogCmd fetches a battle's full log. Output is JSON unless the
// session is in styled mode, because the raw pages are the point: this is the
// escape hatch for a fight whose creatures the guide cannot name, where the
// derived wildlife_attacks rows come out empty but the damage is still in the
// log.
func getBattleLogCmd(ctx context.Context, out io.Writer, client game.GameClient, args []string, format outputFormat) error {
	if len(args) == 0 || args[0] == "" {
		return fmt.Errorf("usage: get_battle_log <battle_id>")
	}
	battleID := args[0]

	if format != formatStyled {
		pages, err := battlereplay.FetchLog(ctx, client, battleID, battlereplay.MaxLogLimit, nil)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")

		return enc.Encode(pages)
	}

	m, err := battlereplay.FetchModel(ctx, client, battleID, nil)
	if err != nil {
		return err
	}

	return writeBattleDamageSummary(out, m)
}

// writeBattleDamageSummary prints one line per shooter/weapon/damage-type seen,
// naming creatures by participant regardless of whether the field guide knows
// the species -- that is what makes it useful when the capture files nothing.
func writeBattleDamageSummary(out io.Writer, m *battlereplay.ReplayModel) error {
	if m == nil {
		return fmt.Errorf("battle log produced no model")
	}
	kindOf := make(map[string]string, len(m.Participants))
	nameOf := make(map[string]string, len(m.Participants))
	creatures := 0
	for _, p := range m.Participants {
		kindOf[p.PlayerID] = p.Kind
		nameOf[p.PlayerID] = p.Username
		if p.Kind == "creature" {
			creatures++
		}
	}
	//nolint:errcheck // writing a report to stdout; a failed write is not actionable here
	fmt.Fprintf(out, "battle %s: %d tick(s), %d participant(s), %d creature(s)\n",
		m.BattleID, len(m.Frames), len(m.Participants), creatures)

	type key struct{ from, weapon, dmgType string }
	type agg struct {
		shots, hits int
		total       float64
		minD, maxD  float64
	}
	rows := map[key]*agg{}
	for _, f := range m.Frames {
		for _, sh := range f.Shots {
			k := key{sh.FromID, sh.WeaponName, sh.DamageType}
			a, ok := rows[k]
			if !ok {
				a = &agg{}
				rows[k] = a
			}
			a.shots++
			if !sh.Hit {
				continue
			}
			d := float64(sh.Damage)
			a.hits++
			a.total += d
			if a.hits == 1 || d < a.minD {
				a.minD = d
			}
			if d > a.maxD {
				a.maxD = d
			}
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "  no shots recorded in this log") //nolint:errcheck

		return nil
	}
	keys := make([]key, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].from != keys[j].from {
			return keys[i].from < keys[j].from
		}
		if keys[i].weapon != keys[j].weapon {
			return keys[i].weapon < keys[j].weapon
		}

		return keys[i].dmgType < keys[j].dmgType
	})
	//nolint:errcheck // ditto
	fmt.Fprintf(out, "%-22s %-8s %-18s %-12s %6s %6s %10s %8s %8s\n",
		"shooter", "kind", "weapon", "damage_type", "shots", "hits", "total", "min", "max")
	for _, k := range keys {
		a := rows[k]
		name := nameOf[k.from]
		if name == "" {
			name = k.from
		}
		//nolint:errcheck // ditto
		fmt.Fprintf(out, "%-22s %-8s %-18s %-12s %6d %6d %10.0f %8.0f %8.0f\n",
			truncate(name, 22), kindOf[k.from], truncate(k.weapon, 18), truncate(k.dmgType, 12),
			a.shots, a.hits, a.total, a.minD, a.maxD)
	}

	return nil
}
