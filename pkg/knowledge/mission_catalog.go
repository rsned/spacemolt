package knowledge

import (
	"encoding/json"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// MissionFieldDiff records one field whose value changed between the stored
// catalog row and a newly-observed entry.
type MissionFieldDiff struct {
	Field    string
	OldValue string
	NewValue string
}

// MissionUpsertResult summarizes the outcome of UpsertMissionTemplate.
//
//   - Inserted is true when a brand new template_id was stored.
//   - Diffs is non-empty when an existing row had different values; the row
//     is always updated to the new values before returning.
type MissionUpsertResult struct {
	Inserted bool
	Diffs    []MissionFieldDiff
}

// missionCatalogRow is the normalized, backend-agnostic representation of a
// mission template as stored in the catalog. JSON blobs are kept as their
// canonical string form so diffing is a trivial string comparison.
type missionCatalogRow struct {
	ID              string
	Procedural      bool
	Title           string
	Description     string
	Type            string
	Difficulty      int
	GiverName       string
	GiverTitle      string
	FactionID       string
	FactionName     string
	DialogOffer     string
	DialogAccept    string
	DialogDecline   string
	DialogComplete  string
	ChainNext       string
	Repeatable      bool
	ExpiresInTicks  int
	RewardsCredits  int
	RewardsSkillXP  string // JSON object
	RewardsItems    string // JSON object
	Requirements    string // JSON object
	RequiredModules string // JSON array
	ProvidedItems   string // JSON object
	Objectives      string // JSON array of objectiveRow
}

// objectiveRow is the catalog-side representation of a mission objective.
// Stored as JSON inside missionCatalogRow.Objectives for diffing; expanded
// into the mission_objectives table on SQLite writes.
type objectiveRow struct {
	SortOrder      int    `json:"sort_order"`
	Type           string `json:"type"`
	Description    string `json:"description,omitempty"`
	ItemID         string `json:"item_id,omitempty"`
	Quantity       int    `json:"quantity,omitempty"`
	SystemID       string `json:"system_id,omitempty"`
	SystemName     string `json:"system_name,omitempty"`
	TargetBaseID   string `json:"target_base_id,omitempty"`
	TargetBaseName string `json:"target_base_name,omitempty"`
}

func jsonMarshalString(v any, empty string) string {
	if v == nil {
		return empty
	}
	b, err := json.Marshal(v)
	if err != nil {
		return empty
	}
	return string(b)
}

func jsonUnmarshalString(s string, v any) error {
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

// MissionCatalogID returns the catalog key for a board entry and whether it is
// a procedural (route-generated) mission. Hand-authored missions carry a stable
// template_id and are keyed by it (procedural=false). Procedural missions
// (couriers, trade-runs) carry no template_id; their mission_id embeds a
// per-instance "~<hash>" suffix, so the key is the mission_id with that suffix
// stripped (procedural=true) — which dedups repeat sightings of the same route.
// id is "" when the entry has neither a template_id nor a mission_id (caller
// must skip such entries).
func MissionCatalogID(e serverapi.MissionBoardEntry) (id string, procedural bool) {
	if e.TemplateID != "" {
		return e.TemplateID, false
	}
	id = e.MissionID
	if i := strings.IndexByte(id, '~'); i >= 0 {
		id = id[:i]
	}
	return id, true
}

// missionRowFromEntry converts a game-protocol MissionBoardEntry to the
// catalog row form. Callers must skip entries whose MissionCatalogID is empty
// before calling this.
func missionRowFromEntry(e serverapi.MissionBoardEntry) missionCatalogRow {
	id, procedural := MissionCatalogID(e)
	row := missionCatalogRow{
		ID:              id,
		Procedural:      procedural,
		Title:           e.Title,
		Description:     e.Description,
		Type:            e.Type,
		Difficulty:      e.Difficulty,
		GiverName:       e.Giver.Name,
		GiverTitle:      e.Giver.Title,
		FactionID:       e.FactionID,
		FactionName:     e.FactionName,
		ChainNext:       e.ChainNext,
		Repeatable:      e.Repeatable,
		ExpiresInTicks:  e.ExpiresInTicks,
		RewardsSkillXP:  "{}",
		RewardsItems:    "{}",
		Requirements:    "{}",
		RequiredModules: "[]",
		ProvidedItems:   "{}",
	}
	if e.Dialog != nil {
		row.DialogOffer = e.Dialog.Offer
		row.DialogAccept = e.Dialog.Accept
		row.DialogDecline = e.Dialog.Decline
		row.DialogComplete = e.Dialog.Complete
	}
	if e.Rewards != nil {
		row.RewardsCredits = e.Rewards.Credits
		row.RewardsSkillXP = jsonMarshalString(e.Rewards.SkillXP, "{}")
		row.RewardsItems = jsonMarshalString(e.Rewards.Items, "{}")
	}
	if e.Requirements != nil {
		row.Requirements = jsonMarshalString(e.Requirements, "{}")
	}
	if len(e.RequiredModules) > 0 {
		row.RequiredModules = jsonMarshalString(e.RequiredModules, "[]")
	}
	if len(e.ProvidedItems) > 0 {
		row.ProvidedItems = jsonMarshalString(e.ProvidedItems, "{}")
	}
	objs := make([]objectiveRow, len(e.Objectives))
	for i, o := range e.Objectives {
		objs[i] = objectiveRow{
			SortOrder:      i,
			Type:           o.Type,
			Description:    o.Description,
			ItemID:         o.ItemID,
			Quantity:       o.Quantity,
			SystemID:       o.SystemID,
			SystemName:     o.SystemName,
			TargetBaseID:   o.TargetBaseID,
			TargetBaseName: o.TargetBaseName,
		}
	}
	row.Objectives = jsonMarshalString(objs, "[]")
	return row
}

// objectivesFromRow decodes the JSON-encoded objectives list. Used by the
// SQLite backend to populate mission_objectives rows.
func objectivesFromRow(row missionCatalogRow) []objectiveRow { //nolint:unused // called by Task 5 SQLite upsert
	var out []objectiveRow
	_ = json.Unmarshal([]byte(row.Objectives), &out)
	return out
}

// diffMissionRows returns the list of fields that differ between old and new.
func diffMissionRows(old, new missionCatalogRow) []MissionFieldDiff {
	var diffs []MissionFieldDiff
	add := func(field, o, n string) {
		if o != n {
			diffs = append(diffs, MissionFieldDiff{Field: field, OldValue: o, NewValue: n})
		}
	}
	addInt := func(field string, o, n int) {
		if o != n {
			diffs = append(diffs, MissionFieldDiff{
				Field:    field,
				OldValue: missionItoa(o),
				NewValue: missionItoa(n),
			})
		}
	}
	addBool := func(field string, o, n bool) {
		if o != n {
			diffs = append(diffs, MissionFieldDiff{
				Field:    field,
				OldValue: missionBtoa(o),
				NewValue: missionBtoa(n),
			})
		}
	}

	add("title", old.Title, new.Title)
	add("description", old.Description, new.Description)
	add("type", old.Type, new.Type)
	addInt("difficulty", old.Difficulty, new.Difficulty)
	add("giver_name", old.GiverName, new.GiverName)
	add("giver_title", old.GiverTitle, new.GiverTitle)
	add("faction_id", old.FactionID, new.FactionID)
	add("faction_name", old.FactionName, new.FactionName)
	add("dialog_offer", old.DialogOffer, new.DialogOffer)
	add("dialog_accept", old.DialogAccept, new.DialogAccept)
	add("dialog_decline", old.DialogDecline, new.DialogDecline)
	add("dialog_complete", old.DialogComplete, new.DialogComplete)
	add("chain_next", old.ChainNext, new.ChainNext)
	addBool("repeatable", old.Repeatable, new.Repeatable)
	addInt("expires_in_ticks", old.ExpiresInTicks, new.ExpiresInTicks)
	addInt("rewards_credits", old.RewardsCredits, new.RewardsCredits)
	add("rewards_skill_xp", old.RewardsSkillXP, new.RewardsSkillXP)
	add("rewards_items", old.RewardsItems, new.RewardsItems)
	add("requirements", old.Requirements, new.Requirements)
	add("required_modules", old.RequiredModules, new.RequiredModules)
	add("provided_items", old.ProvidedItems, new.ProvidedItems)
	add("objectives", old.Objectives, new.Objectives)

	return diffs
}

func missionItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func missionBtoa(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
