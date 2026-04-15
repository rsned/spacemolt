-- Analysis of Spurious XP Observations Bug
-- ==========================================
-- This script analyzes and identifies spurious XP observations caused by the bug
-- where beforeXP or beforeSkills were nil, causing false positive change detection.
--
-- AFFECTED AGENT: craftsman-1
-- AFFECTED SKILLS: smuggling, nebula_attunement
--
-- The bug caused non-mission actions (survey_system, travel, find_route, refuel, etc.)
-- to incorrectly log XP deltas when they shouldn't have.

-- ============================================================================
-- SUMMARY OF AFFECTED RECORDS
-- ============================================================================

-- Total impact by skill
SELECT '=== SUMMARY: Total Recorded vs Actual XP ===' as report_section;
SELECT
  skill_id,
  COUNT(*) as total_records,
  SUM(xp_delta) as total_recorded_xp,
  'CHECK MANUALLY' as actual_xp_note,
  SUM(CASE WHEN source = 'mission_reward' THEN xp_delta ELSE 0 END) as legitimate_mission_xp
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id IN ('smuggling', 'nebula_attunement')
GROUP BY skill_id
ORDER BY skill_id;

-- Detailed breakdown by action for each affected skill
SELECT '=== BREAKDOWN BY ACTION: Smuggling ===' as report_section;
SELECT
  action,
  source,
  COUNT(*) as record_count,
  SUM(xp_delta) as total_xp,
  MIN(created_at) as first_occurrence,
  MAX(created_at) as last_occurrence
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id = 'smuggling'
GROUP BY action, source
ORDER BY MIN(created_at);

SELECT '=== BREAKDOWN BY ACTION: Nebula Attunement ===' as report_section;
SELECT
  action,
  source,
  COUNT(*) as record_count,
  SUM(xp_delta) as total_xp,
  MIN(created_at) as first_occurrence,
  MAX(created_at) as last_occurrence
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id = 'nebula_attunement'
GROUP BY action, source
ORDER BY MIN(created_at);

-- ============================================================================
-- IDENTIFY SPURIOUS RECORDS
-- ============================================================================

-- Records that are definitely spurious (pattern: non-mission with XP delta > 0)
SELECT '=== SPURIOUS RECORDS: Non-Mission Actions with XP ===' as report_section;
SELECT
  id,
  action,
  target,
  source,
  xp_delta,
  level_before,
  level_after,
  game_tick,
  created_at
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id IN ('smuggling', 'nebula_attunement')
  AND source != 'mission_reward'
  AND action NOT IN ('jump') -- Keep original jump records (pre-tracking)
ORDER BY skill_id, game_tick;

-- ============================================================================
-- CONFIRMED LEGITIMATE RECORDS
-- ============================================================================

SELECT '=== LEGITIMATE RECORDS: Mission Rewards ===' as report_section;
SELECT
  id,
  action,
  target,
  source,
  xp_delta,
  game_tick,
  created_at
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id IN ('smuggling', 'nebula_attunement')
  AND source = 'mission_reward'
ORDER BY skill_id, game_tick;

-- ============================================================================
-- VERIFICATION QUERY
-- ============================================================================

-- After cleanup, run this to verify the data is correct
SELECT '=== VERIFICATION: Post-Cleanup Summary ===' as report_section;
-- This query would be run after cleanup to show the corrected state
SELECT
  'Total records after cleanup:' as description,
  COUNT(*) as count
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id IN ('smuggling', 'nebula_attunement');

SELECT
  'Total XP after cleanup:' as description,
  SUM(xp_delta) as total_xp
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id IN ('smuggling', 'nebula_attunement');

-- ============================================================================
-- CLEANUP QUERIES (USE WITH CAUTION)
-- ============================================================================

-- WARNING: These queries will DELETE data. Review the results above first!

-- Delete spurious records for smuggling (keep only mission_reward)
-- DELETE FROM xp_observations
-- WHERE agent_id = 'craftsman-1'
--   AND skill_id = 'smuggling'
--   AND source != 'mission_reward'
--   AND id IN (
--     -- Specific IDs to delete: 258, 268, 307, 325, 338, 361
--     SELECT id FROM xp_observations
--     WHERE agent_id = 'craftsman-1'
--       AND skill_id = 'smuggling'
--       AND source != 'mission_reward'
--       AND action != 'jump'
--   );

-- Delete spurious records for nebula_attunement (keep only mission_reward and original jumps)
-- DELETE FROM xp_observations
-- WHERE agent_id = 'craftsman-1'
--   AND skill_id = 'nebula_attunement'
--   AND source != 'mission_reward'
--   AND id IN (
--     -- Specific IDs to delete: 174, 259, 269, 308, 326, 339, 362
--     -- Keep: 53, 69, 85, 101 (original jumps), 210 (mission)
--     SELECT id FROM xp_observations
--     WHERE agent_id = 'craftsman-1'
--       AND skill_id = 'nebula_attunement'
--       AND source != 'mission_reward'
--       AND action NOT IN ('jump')
--   );

-- ============================================================================
-- EXPECTED FINAL STATE
-- ============================================================================

SELECT '=== EXPECTED FINAL STATE ===' as report_section;
SELECT
  'smuggling' as skill_id,
  '1 record (mission_reward): +50 XP' as expected_state,
  'Current total: 50 XP (35 mission + 15 initial)' as actual_skill_value
UNION ALL
SELECT
  'nebula_attunement' as skill_id,
  '5 records (4 jumps + 1 mission): +60 XP total' as expected_state,
  'Current total: 50 XP (35 mission + 15 initial)' as actual_skill_value;
