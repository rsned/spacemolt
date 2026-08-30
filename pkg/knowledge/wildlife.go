package knowledge

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Wildlife sighting sources. A get_nearby sighting is a real headcount at one
// POI; a survey_system sighting is the server's own estimate for a whole system
// and names no POI. Mixing them in one average would be meaningless, so every
// row records which it is.
const (
	WildlifeSourceNearby = "get_nearby"
	WildlifeSourceSurvey = "survey_system"
)

// WildlifeSpecies is one entry in the field guide — the properties that hold for
// every individual of a species, accumulated from whatever we have seen.
//
// The game publishes no wildlife catalog, so this table is only ever as complete
// as our own observation. Danger is the one field that cannot be had for free:
// it comes from scan, which is a mutation costing a tick, so DangerScannedUTC
// empty doubles as the work list of species still to scan.
type WildlifeSpecies struct {
	Species string
	Name    string
	Role    string
	// MaxHull is the largest max_hull seen for the species. Individuals of a
	// species have all shown identical max_hull so far (every Cauldronback 120,
	// every Belt-Grazer 60), so this behaves as a species constant — but it is
	// recorded as an observed maximum rather than asserted as a constant.
	MaxHull int
	// MaxShield is recorded rather than assumed. The docs say creatures carry
	// hull and armor but no shields, and the one apex measured so far (Rainbow
	// Leviathan, 2200 hull) confirmed max_shield 0 — but a shielded species
	// would otherwise be indistinguishable from an unobserved one.
	MaxShield int
	// Danger is whatever scan reports for the species. No creature has been
	// scanned yet, so the field's shape and value set are both unknown; it is
	// TEXT so that a rating like "moderate" and a number both survive.
	Danger           string
	DangerScannedUTC string
	// ScanTraits is the verbatim text after the em dash in a scan's display
	// string ("harmless prey, ranchable stock"); ScanRevealed is that scan's
	// revealed_info list, comma-joined. Both are stored raw because the trait
	// vocabulary is unknown — every grazer measured so far reads "harmless
	// prey", and an apex predator will presumably not.
	ScanTraits   string
	ScanRevealed string
	// Description is the species' lore, read off a scan (v0.571.0). Kept once
	// seen: a later lore-less scan never erases it.
	Description string
	// Ranchable is derived from ScanRevealed containing "ranchable". It is only
	// meaningful once DangerScannedUTC is set: false on an unscanned species
	// means unknown, not "cannot be ranched".
	Ranchable bool
	// Habitats are the POI types the species has been seen at (belt, gas_cloud,
	// cryobelt, nebula...), deduplicated. Diet is not directly readable, but the
	// docs say each grazer eats one specific ore or gas, so habitat plus that
	// POI's resources is the evidence a diet can eventually be inferred from.
	Habitats     []string
	FirstSeenUTC string
	LastSeenUTC  string
}

// WildlifeSighting is one observation of one species in one place at one time.
//
// ObservedCount means different things per source and that is deliberate: for
// get_nearby it is the number of individuals actually counted at POIID, for
// survey_system it is the server's system-wide estimate and POIID is empty.
type WildlifeSighting struct {
	Species        string
	SystemID       string
	POIID          string
	Source         string
	ObservedCount  int
	Abundance      string
	Ranched        int
	Branded        int
	InCombat       int
	BloomStatus    string
	BloomIntensity float64
	// SurveyPower is the surveyor's scanning strength on a survey_system row,
	// and 0 on a get_nearby one, which counts individuals rather than
	// estimating. The census is an ESTIMATE produced by that power, so two
	// agents surveying the same system on the same tick can disagree; without
	// this field a series assembled across agents cannot tell a population
	// change from a change of surveyor.
	SurveyPower int
	GameTick    int64
	ObservedUTC string
	AgentID     string
}

// WildlifeKill is one dead creature: what it was, what it cost to kill, and
// whether we ever got to look inside the carcass.
//
// CarcassRead is the load-bearing field for drop statistics. A kill whose
// carcass we never read tells us nothing about drops and must stay out of the
// denominator; a kill whose carcass we read and found EMPTY is a real
// zero-drop observation and must stay in it. Counting kills instead of
// carcasses conflates the two and biases every drop rate downward.
type WildlifeKill struct {
	CreatureID    string
	GameTick      int64
	Species       string
	CreatureName  string
	Role          string
	MaxHull       int
	SystemID      string
	POIID         string
	BattleID      string
	DurationTicks int
	DamageDealt   int
	DamageTaken   int
	WreckID       string
	SalvageValue  int
	CarcassRead   bool
	KilledUTC     string
	AgentID       string
	// Drops are the carcass CONTENTS as get_wrecks reported them, which is the
	// unbiased result of the drop roll. Never populate this from what was
	// actually looted: huntLootCarcass caps each item at remaining hold space
	// and falls back to salvage when the hold is full, so a full-holded agent
	// would report a smaller drop table for the same species than an empty one.
	Drops []WildlifeDrop
}

// WildlifeDrop is one item line on a carcass.
type WildlifeDrop struct {
	ItemID   string
	Quantity float64
}

// WildlifeDropRate is the rolled-up drop table for one species and item:
// how often the item appeared, out of how many carcasses were actually read.
//
// This is computed on read and never stored. A stored rate would be wrong the
// moment the next carcass is opened, and could not be recomputed if the
// capture path changed.
type WildlifeDropRate struct {
	Species string
	ItemID  string
	// Carcasses is the denominator: carcasses read for this species.
	Carcasses int
	// Appearances is how many of those carcasses held this item at all.
	Appearances int
	// TotalQuantity is summed across appearances; MeanPerDrop divides it by
	// Appearances (the mean GIVEN the item dropped), while MeanPerCarcass
	// divides by Carcasses (the expected yield per kill).
	TotalQuantity  float64
	MeanPerDrop    float64
	MeanPerCarcass float64
	// MinQuantity/MaxQuantity bracket the observed line size, which is how a
	// variable roll ("sometimes 1, sometimes 2") shows itself.
	MinQuantity float64
	MaxQuantity float64
}

// UpsertWildlifeSpecies merges observations of a species into the field guide.
//
// Merging rather than replacing: any single sighting knows only part of the
// truth (get_nearby has hull and no abundance, survey_system the reverse), so a
// blank field never overwrites a known one, MaxHull only ratchets up, and
// Habitats accumulate. FirstSeenUTC is preserved once set.
func (kb *SQLiteKB) UpsertWildlifeSpecies(ctx context.Context, rows []WildlifeSpecies) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)

	// The habitat union is computed in Go, not SQL: SQLite has no set type, and
	// unioning a comma-joined string in SQL would need a recursive CTE for
	// something this small. Read before the transaction — txer only exposes
	// ExecContext, and a habitat list is an accumulating convenience rather
	// than a correctness input, so a racing writer costs at worst one repeated
	// observation.
	existing := make(map[string][]string, len(rows))
	for _, r := range rows {
		if r.Species == "" {
			continue
		}
		var habitats string
		err := kb.db.QueryRowContext(ctx,
			`SELECT habitats FROM wildlife_species WHERE species = ?`, r.Species).Scan(&habitats)
		if err == nil {
			existing[r.Species] = strings.Split(habitats, ",")
		}
	}

	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if r.Species == "" {
				continue
			}
			seen := r.LastSeenUTC
			if seen == "" {
				seen = now
			}
			first := r.FirstSeenUTC
			if first == "" {
				first = seen
			}
			habitats := strings.Join(
				normalizeHabitats(append(existing[r.Species], r.Habitats...)), ",")
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO wildlife_species
					(species, name, role, max_hull, max_shield, danger, danger_scanned_utc,
					 scan_traits, scan_revealed, ranchable, description,
					 habitats, first_seen_utc, last_seen_utc)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(species) DO UPDATE SET
					name               = CASE WHEN excluded.name  <> '' THEN excluded.name  ELSE wildlife_species.name END,
					role               = CASE WHEN excluded.role  <> '' THEN excluded.role  ELSE wildlife_species.role END,
					max_hull           = MAX(wildlife_species.max_hull, excluded.max_hull),
					max_shield         = MAX(wildlife_species.max_shield, excluded.max_shield),
					danger             = CASE WHEN excluded.danger <> '' THEN excluded.danger ELSE wildlife_species.danger END,
					danger_scanned_utc = CASE WHEN excluded.danger_scanned_utc <> '' THEN excluded.danger_scanned_utc ELSE wildlife_species.danger_scanned_utc END,
					scan_traits        = CASE WHEN excluded.scan_traits   <> '' THEN excluded.scan_traits   ELSE wildlife_species.scan_traits END,
					scan_revealed      = CASE WHEN excluded.scan_revealed <> '' THEN excluded.scan_revealed ELSE wildlife_species.scan_revealed END,
					ranchable          = MAX(wildlife_species.ranchable, excluded.ranchable),
					description        = CASE WHEN excluded.description <> '' THEN excluded.description ELSE wildlife_species.description END,
					habitats           = excluded.habitats,
					last_seen_utc      = excluded.last_seen_utc
			`, r.Species, r.Name, r.Role, r.MaxHull, r.MaxShield, r.Danger, r.DangerScannedUTC,
				r.ScanTraits, r.ScanRevealed, boolInt(r.Ranchable), r.Description,
				habitats, first, seen); err != nil {
				return fmt.Errorf("upsert wildlife species %s: %w", r.Species, err)
			}
		}
		return nil
	})
}

// normalizeHabitats trims, drops empties, deduplicates and sorts a habitat
// list so the stored string is stable regardless of observation order.
func normalizeHabitats(in []string) []string {
	out := make([]string, 0, len(in))
	for _, h := range in {
		h = strings.TrimSpace(h)
		if h != "" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	slices.Sort(out)
	return out
}

// GetWildlifeSpecies returns the whole field guide, ordered by species id.
func (kb *SQLiteKB) GetWildlifeSpecies(ctx context.Context) ([]WildlifeSpecies, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT species, name, role, max_hull, max_shield, danger, danger_scanned_utc,
		       scan_traits, scan_revealed, ranchable, description,
		       habitats, first_seen_utc, last_seen_utc
		FROM wildlife_species
		ORDER BY species
	`)
	if err != nil {
		return nil, fmt.Errorf("query wildlife species: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeSpecies
	for rows.Next() {
		var s WildlifeSpecies
		var habitats string
		if err := rows.Scan(&s.Species, &s.Name, &s.Role, &s.MaxHull, &s.MaxShield,
			&s.Danger, &s.DangerScannedUTC, &s.ScanTraits, &s.ScanRevealed,
			&s.Ranchable, &s.Description, &habitats, &s.FirstSeenUTC, &s.LastSeenUTC); err != nil {
			return nil, fmt.Errorf("scan wildlife species: %w", err)
		}
		s.Habitats = normalizeHabitats(strings.Split(habitats, ","))
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetUnscannedWildlifeSpecies lists species whose danger rating has never been
// read off a scan. This is the scan campaign's work list: danger is a species
// property, so it needs one scan per species ever — not one per creature, which
// at 1 tick per scan would cost minutes per herd.
func (kb *SQLiteKB) GetUnscannedWildlifeSpecies(ctx context.Context) ([]string, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT species FROM wildlife_species
		WHERE danger_scanned_utc = '' OR danger_scanned_utc IS NULL
		ORDER BY species
	`)
	if err != nil {
		return nil, fmt.Errorf("query unscanned wildlife species: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan unscanned species: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecordWildlifeSightings appends census rows. Sightings are append-only: two
// observations of the same herd minutes apart are two facts, not a correction,
// and blooms and migration are only visible as a series.
func (kb *SQLiteKB) RecordWildlifeSightings(ctx context.Context, rows []WildlifeSighting) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if r.Species == "" {
				continue
			}
			observed := r.ObservedUTC
			if observed == "" {
				observed = now
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO wildlife_sightings
					(species, system_id, poi_id, source, observed_count, abundance,
					 ranched, branded, in_combat, bloom_status, bloom_intensity,
					 survey_power, game_tick, observed_utc, agent_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, r.Species, r.SystemID, r.POIID, r.Source, r.ObservedCount, r.Abundance,
				r.Ranched, r.Branded, r.InCombat, r.BloomStatus, r.BloomIntensity,
				r.SurveyPower, r.GameTick, observed, r.AgentID); err != nil {
				return fmt.Errorf("record wildlife sighting %s: %w", r.Species, err)
			}
		}
		return nil
	})
}

// GetWildlifeSightings returns the most recent sightings for a species, newest
// first. An empty species returns sightings of every species.
func (kb *SQLiteKB) GetWildlifeSightings(ctx context.Context, species string, limit int) ([]WildlifeSighting, error) {
	if limit < 1 {
		limit = 100
	}
	rows, err := kb.db.QueryContext(ctx, `
		SELECT species, system_id, poi_id, source, observed_count, abundance,
		       ranched, branded, in_combat, bloom_status, bloom_intensity,
		       survey_power, game_tick, observed_utc, agent_id
		FROM wildlife_sightings
		WHERE (? = '' OR species = ?)
		ORDER BY observed_utc DESC, id DESC
		LIMIT ?
	`, species, species, limit)
	if err != nil {
		return nil, fmt.Errorf("query wildlife sightings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeSighting
	for rows.Next() {
		var s WildlifeSighting
		if err := rows.Scan(&s.Species, &s.SystemID, &s.POIID, &s.Source,
			&s.ObservedCount, &s.Abundance, &s.Ranched, &s.Branded, &s.InCombat,
			&s.BloomStatus, &s.BloomIntensity, &s.SurveyPower, &s.GameTick,
			&s.ObservedUTC, &s.AgentID); err != nil {
			return nil, fmt.Errorf("scan wildlife sighting: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecordWildlifeKill stores a kill and its carcass contents together, keyed on
// (creature_id, game_tick) so re-recording the same kill is idempotent rather
// than double-counting it in the drop denominator.
//
// The tick is part of the key rather than the creature id alone because it is
// not known whether the server recycles crt_ ids after a respawn; keying on
// both means a recycled id lands as a separate kill instead of overwriting the
// original observation.
func (kb *SQLiteKB) RecordWildlifeKill(ctx context.Context, k WildlifeKill) error {
	if k.CreatureID == "" {
		return fmt.Errorf("record wildlife kill: empty creature id")
	}
	killed := k.KilledUTC
	if killed == "" {
		killed = time.Now().UTC().Format(time.RFC3339)
	}
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wildlife_kills
				(creature_id, game_tick, species, creature_name, role, max_hull,
				 system_id, poi_id, battle_id, duration_ticks, damage_dealt,
				 damage_taken, wreck_id, salvage_value, carcass_read, killed_utc, agent_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(creature_id, game_tick) DO UPDATE SET
				species        = CASE WHEN excluded.species <> '' THEN excluded.species ELSE wildlife_kills.species END,
				creature_name  = CASE WHEN excluded.creature_name <> '' THEN excluded.creature_name ELSE wildlife_kills.creature_name END,
				role           = CASE WHEN excluded.role <> '' THEN excluded.role ELSE wildlife_kills.role END,
				max_hull       = MAX(wildlife_kills.max_hull, excluded.max_hull),
				system_id      = CASE WHEN excluded.system_id <> '' THEN excluded.system_id ELSE wildlife_kills.system_id END,
				poi_id         = CASE WHEN excluded.poi_id <> '' THEN excluded.poi_id ELSE wildlife_kills.poi_id END,
				battle_id      = CASE WHEN excluded.battle_id <> '' THEN excluded.battle_id ELSE wildlife_kills.battle_id END,
				duration_ticks = MAX(wildlife_kills.duration_ticks, excluded.duration_ticks),
				damage_dealt   = MAX(wildlife_kills.damage_dealt, excluded.damage_dealt),
				damage_taken   = MAX(wildlife_kills.damage_taken, excluded.damage_taken),
				wreck_id       = CASE WHEN excluded.wreck_id <> '' THEN excluded.wreck_id ELSE wildlife_kills.wreck_id END,
				salvage_value  = MAX(wildlife_kills.salvage_value, excluded.salvage_value),
				carcass_read   = MAX(wildlife_kills.carcass_read, excluded.carcass_read),
				agent_id       = CASE WHEN excluded.agent_id <> '' THEN excluded.agent_id ELSE wildlife_kills.agent_id END
		`, k.CreatureID, k.GameTick, k.Species, k.CreatureName, k.Role, k.MaxHull,
			k.SystemID, k.POIID, k.BattleID, k.DurationTicks, k.DamageDealt,
			k.DamageTaken, k.WreckID, k.SalvageValue, boolToInt(k.CarcassRead),
			killed, k.AgentID); err != nil {
			return fmt.Errorf("record wildlife kill %s: %w", k.CreatureID, err)
		}

		for _, d := range k.Drops {
			if d.ItemID == "" {
				continue
			}
			// Quantity is replaced, not summed: a re-read of the same carcass
			// reports the same contents, so adding would inflate the yield.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO wildlife_kill_drops (creature_id, game_tick, item_id, quantity)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(creature_id, game_tick, item_id) DO UPDATE SET
					quantity = excluded.quantity
			`, k.CreatureID, k.GameTick, d.ItemID, d.Quantity); err != nil {
				return fmt.Errorf("record wildlife drop %s/%s: %w", k.CreatureID, d.ItemID, err)
			}
		}
		return nil
	})
}

// GetWildlifeDropRates rolls the raw kill and drop rows up into a drop table
// for one species (or every species, when species is empty).
//
// The denominator counts only carcasses actually READ (carcass_read = 1). An
// empty carcass that was read still counts — that is what makes a rate below
// 100% mean anything.
func (kb *SQLiteKB) GetWildlifeDropRates(ctx context.Context, species string) ([]WildlifeDropRate, error) {
	rows, err := kb.db.QueryContext(ctx, `
		WITH read_kills AS (
			SELECT creature_id, game_tick, species
			FROM wildlife_kills
			WHERE carcass_read = 1 AND (? = '' OR species = ?)
		),
		denom AS (
			SELECT species, COUNT(*) AS carcasses FROM read_kills GROUP BY species
		)
		SELECT k.species, d.item_id, denom.carcasses, COUNT(*) AS appearances,
		       SUM(d.quantity), MIN(d.quantity), MAX(d.quantity)
		FROM read_kills k
		JOIN wildlife_kill_drops d
		  ON d.creature_id = k.creature_id AND d.game_tick = k.game_tick
		JOIN denom ON denom.species = k.species
		GROUP BY k.species, d.item_id
		ORDER BY k.species, appearances DESC, d.item_id
	`, species, species)
	if err != nil {
		return nil, fmt.Errorf("query wildlife drop rates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeDropRate
	for rows.Next() {
		var r WildlifeDropRate
		if err := rows.Scan(&r.Species, &r.ItemID, &r.Carcasses, &r.Appearances,
			&r.TotalQuantity, &r.MinQuantity, &r.MaxQuantity); err != nil {
			return nil, fmt.Errorf("scan wildlife drop rate: %w", err)
		}
		if r.Appearances > 0 {
			r.MeanPerDrop = r.TotalQuantity / float64(r.Appearances)
		}
		if r.Carcasses > 0 {
			r.MeanPerCarcass = r.TotalQuantity / float64(r.Carcasses)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountWildlifeCarcassesRead returns how many carcasses have been read for a
// species — the denominator behind every rate in GetWildlifeDropRates, exposed
// on its own so a caller can tell "no drops seen" from "no carcasses opened".
func (kb *SQLiteKB) CountWildlifeCarcassesRead(ctx context.Context, species string) (int, error) {
	var n int
	err := kb.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM wildlife_kills
		WHERE carcass_read = 1 AND (? = '' OR species = ?)
	`, species, species).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count wildlife carcasses read: %w", err)
	}
	return n, nil
}

// boolInt maps a Go bool onto the INTEGER SQLite stores it as. (seen_players.go
// has boolToInt for the same job; this file cannot reuse the name without
// shadowing it across the package.)
func boolInt(b bool) int {
	if b {
		return 1
	}

	return 0
}
