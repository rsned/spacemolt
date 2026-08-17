package game

// rememberBattleIDLocked records the id of a battle this client is or was in.
//
// The caller must hold c.mu.
//
// An empty id is ignored rather than clearing the field: battle_damage carries
// no battle_id at all, and several replies omit it, so overwriting on every
// battle event would erase the handle precisely when a fight is in progress.
// The id therefore survives the battle it names, which is the point — a death
// clears BattleState, and the log of the battle that killed you is only
// reachable through an id you kept.
func (c *Client) rememberBattleIDLocked(battleID string) {
	if battleID == "" {
		return
	}
	c.state.LastBattleID = battleID
}
