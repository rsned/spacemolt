-- ========================================================================
-- VIEW XP OBSERVATION DATA
-- ========================================================================
-- Prints aggregated and recent rows from the xp_observations table,
-- which captures skill XP/level deltas attributed to specific commands.
-- Populated by the XP tracker wired into auto-explorer, play_as, and any
-- other agent opted in via knowledge.NewXPTracker.
--
-- Usage:
--   sqlite3 data/spacemolt-knowledge.db < scripts/sql/view_xp.sql
--
-- Or interactively:
--   sqlite3 data/spacemolt-knowledge.db
--   > .read scripts/sql/view_xp.sql
-- ========================================================================

.mode box
.headers on
.nullvalue 'N/A'

-- ========================================================================
-- OVERVIEW
-- ========================================================================
SELECT '=== OVERVIEW ===' AS "==================================================";

SELECT
    COUNT(*)                        AS total_observations,
    COUNT(DISTINCT agent_id)        AS distinct_agents,
    COUNT(DISTINCT action)          AS distinct_actions,
    COUNT(DISTINCT skill_id)        AS distinct_skills,
    ROUND(SUM(xp_delta), 1)         AS total_xp_recorded,
    SUM(CASE WHEN level_delta > 0 THEN 1 ELSE 0 END) AS level_ups,
    MIN(created_at)                 AS first_observation,
    MAX(created_at)                 AS last_observation
FROM xp_observations;

-- ========================================================================
-- BY AGENT
-- ========================================================================
SELECT '=== BY AGENT ===' AS "==================================================";

SELECT
    agent_id,
    COUNT(*)                                          AS obs,
    COUNT(DISTINCT action)                            AS actions,
    COUNT(DISTINCT skill_id)                          AS skills,
    ROUND(SUM(xp_delta), 1)                           AS total_xp,
    SUM(CASE WHEN level_delta > 0 THEN 1 ELSE 0 END)  AS lvl_ups,
    MIN(created_at)                                   AS first_seen,
    MAX(created_at)                                   AS last_seen
FROM xp_observations
GROUP BY agent_id
ORDER BY total_xp DESC;

-- ========================================================================
-- BY ACTION (which commands generate the most XP)
-- ========================================================================
SELECT '=== BY ACTION (TOP 30 BY TOTAL XP) ===' AS "==================================================";

SELECT
    action,
    source,
    COUNT(*)                                          AS obs,
    COUNT(DISTINCT skill_id)                          AS skills_trained,
    ROUND(SUM(xp_delta), 1)                           AS total_xp,
    ROUND(AVG(xp_delta), 2)                           AS avg_xp_per_obs,
    SUM(CASE WHEN level_delta > 0 THEN 1 ELSE 0 END)  AS lvl_ups
FROM xp_observations
GROUP BY action, source
ORDER BY total_xp DESC
LIMIT 30;

-- ========================================================================
-- BY SKILL (which skills are being trained)
-- ========================================================================
SELECT '=== BY SKILL ===' AS "==================================================";

SELECT
    skill_id,
    COUNT(*)                                          AS obs,
    COUNT(DISTINCT action)                            AS distinct_actions,
    ROUND(SUM(xp_delta), 1)                           AS total_xp,
    ROUND(AVG(xp_delta), 2)                           AS avg_xp_per_obs,
    SUM(CASE WHEN level_delta > 0 THEN 1 ELSE 0 END)  AS lvl_ups,
    MAX(level_after)                                  AS max_level_seen
FROM xp_observations
GROUP BY skill_id
ORDER BY total_xp DESC;

-- ========================================================================
-- ACTION x SKILL CROSSTAB (which skills each action trains)
-- ========================================================================
SELECT '=== ACTION x SKILL (TOP 50) ===' AS "==================================================";

SELECT
    action,
    skill_id,
    source,
    COUNT(*)                AS obs,
    ROUND(SUM(xp_delta), 1) AS total_xp,
    ROUND(AVG(xp_delta), 2) AS avg_xp
FROM xp_observations
GROUP BY action, skill_id, source
ORDER BY total_xp DESC
LIMIT 50;

-- ========================================================================
-- LEVEL-UP EVENTS
-- ========================================================================
SELECT '=== LEVEL-UPS ===' AS "==================================================";

SELECT
    created_at,
    agent_id,
    skill_id,
    level_before || ' -> ' || level_after AS level_change,
    action,
    target,
    game_tick
FROM xp_observations
WHERE level_delta > 0
ORDER BY created_at DESC
LIMIT 50;

-- ========================================================================
-- RECENT OBSERVATIONS (last 50)
-- ========================================================================
SELECT '=== RECENT 50 OBSERVATIONS ===' AS "==================================================";

SELECT
    id,
    created_at,
    agent_id,
    action,
    CASE WHEN target = '' THEN NULL ELSE target END AS target,
    skill_id,
    ROUND(xp_delta, 1) AS xp,
    CASE WHEN level_delta != 0
        THEN level_before || '->' || level_after
        ELSE NULL
    END AS lvl,
    source,
    game_tick
FROM xp_observations
ORDER BY id DESC
LIMIT 50;
