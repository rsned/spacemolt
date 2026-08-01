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

-- captured_at is PER TABLE, not per agent. The sources refresh on different
-- cadences (carrier profile is two free queries; a storage sweep is N calls),
-- and one agent-level timestamp would make a 20-minute-old skill level and a
-- 6-day-old holding indistinguishable.
CREATE TABLE IF NOT EXISTS agent_profile (
    player_id      TEXT PRIMARY KEY,
    username       TEXT NOT NULL DEFAULT '',
    empire         TEXT NOT NULL DEFAULT '',
    credits        REAL NOT NULL DEFAULT 0,
    home_base      TEXT NOT NULL DEFAULT '',
    docked_at_base TEXT NOT NULL DEFAULT '',
    current_system TEXT NOT NULL DEFAULT '',
    current_poi    TEXT NOT NULL DEFAULT '',
    active_ship_id TEXT NOT NULL DEFAULT '',
    faction_id     TEXT NOT NULL DEFAULT '',
    faction_rank   TEXT NOT NULL DEFAULT '',
    experience     INTEGER NOT NULL DEFAULT 0,
    captured_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agent_profile_faction ON agent_profile(faction_id);

CREATE TABLE IF NOT EXISTS agent_skills (
    player_id   TEXT NOT NULL,
    skill       TEXT NOT NULL,
    level       INTEGER NOT NULL DEFAULT 0,
    xp          REAL NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, skill)
);
CREATE INDEX IF NOT EXISTS idx_agent_skills_skill ON agent_skills(skill, level);

-- baseline is the load-bearing column, not reputation: reputation floats above
-- baseline from missions and decays back toward it when idle, so baseline is
-- what makes an unlock permanent (an_introduction raises the pirate baseline
-- from -30 to 10, which is what makes stronghold docking stick).
CREATE TABLE IF NOT EXISTS agent_standings (
    player_id          TEXT NOT NULL,
    faction            TEXT NOT NULL,
    reputation         INTEGER NOT NULL DEFAULT 0,
    baseline           INTEGER NOT NULL DEFAULT 0,
    outstanding_bounty INTEGER NOT NULL DEFAULT 0,
    jailed_until       TEXT NOT NULL DEFAULT '',
    captured_at        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, faction)
);
CREATE INDEX IF NOT EXISTS idx_agent_standings_faction ON agent_standings(faction, baseline);
