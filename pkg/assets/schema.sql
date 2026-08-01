-- Agent asset + capability ledger. Keyed on the server's stable hex player_id.
-- Tables mirror the wire shapes: maps become narrow tables, structs become wide
-- ones, so a new skill or faction is a new ROW and needs no migration.
--
-- No foreign keys to the item/ship catalogs on purpose: the legacy agent_ships
-- table declares FOREIGN KEY(class_id) REFERENCES ships(id) and 96% of observed
-- class_id values do not resolve (prospector/prospect, excavator/excavation).
-- Store class_id verbatim and resolve at read time so a mismatch is visible
-- rather than fatal.

CREATE TABLE IF NOT EXISTS agents (
    player_id  TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL DEFAULT '',
    username   TEXT NOT NULL DEFAULT '',
    first_seen TEXT NOT NULL DEFAULT '',
    last_seen  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agents_agent_id ON agents(agent_id);
