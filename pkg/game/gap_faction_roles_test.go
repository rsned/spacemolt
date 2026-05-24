package game

import "testing"

func TestGapFactionRolesFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"join_faction":           {"faction", "faction_id"},
		"faction_decline_invite": {"faction_id"},
		"faction_get_invites":    {"invites"},
		"faction_create_role":    {"name", "priority", "role_id"},
		"faction_delete_role":    {"reassigned_count", "role_id"},
		"faction_edit_role":      {"role_id", "updates"},
		"faction_edit":           {"hint", "updates"},
		"faction_delete_room":    {"room_id"},
		"faction_visit_room":     {"access", "author", "created_at", "description", "name", "room_id", "updated_at"},
		"faction_write_room":     {"access", "faction", "hint", "name", "room_id"},
	})
}
