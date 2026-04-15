-- Cleanup Script for Spurious XP Observations
-- ==========================================
-- This script removes spurious XP observations caused by the beforeXP/beforeSkills nil bug.
--
-- AFFECTED AGENT: craftsman-1
-- AFFECTED SKILLS: smuggling, nebula_attunement
--
-- SAFETY FEATURES:
-- 1. Uses explicit ID lists (no wildcard DELETEs)
-- 2. Shows what will be deleted before deleting
-- 3. Can be run in sections for verification
-- 4. Creates backup table before deletion

-- ============================================================================
-- STEP 1: Create backup table (safety net)
-- ============================================================================

CREATE TABLE IF NOT EXISTS xp_observations_backup_20260415 AS
SELECT * FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id IN ('smuggling', 'nebula_attunement');

SELECT '=== Backup table created ===' as status;
SELECT COUNT(*) as records_backed_up FROM xp_observations_backup_20260415;

-- ============================================================================
-- STEP 2: Show what will be deleted (review this before proceeding!)
-- ============================================================================

SELECT '=== RECORDS TO DELETE: Smuggling (6 spurious records) ===' as section;
SELECT
  id,
  action,
  target,
  source,
  xp_delta,
  game_tick,
  created_at
FROM xp_observations
WHERE id IN (258, 268, 307, 325, 338, 361)
ORDER BY id;

SELECT '=== RECORDS TO DELETE: Nebula Attunement (7 spurious records) ===' as section;
SELECT
  id,
  action,
  target,
  source,
  xp_delta,
  game_tick,
  created_at
FROM xp_observations
WHERE id IN (174, 259, 269, 308, 326, 339, 362)
ORDER BY id;

-- ============================================================================
-- STEP 3: Perform the deletions (uncomment when ready)
-- ============================================================================

-- Delete spurious smuggling records (keep mission reward ID 209)
-- DELETE FROM xp_observations WHERE id IN (258, 268, 307, 325, 338, 361);

-- Delete spurious nebula_attunement records (keep jumps 53,69,85,101 and mission 210)
-- DELETE FROM xp_observations WHERE id IN (174, 259, 269, 308, 326, 339, 362);

-- ============================================================================
-- STEP 4: Verification queries (run after deletion)
-- ============================================================================

SELECT '=== VERIFICATION: Remaining records for Smuggling ===' as section;
SELECT
  id,
  action,
  source,
  xp_delta,
  created_at
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id = 'smuggling'
ORDER BY id;

SELECT '=== VERIFICATION: Remaining records for Nebula Attunement ===' as section;
SELECT
  id,
  action,
  source,
  xp_delta,
  created_at
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id = 'nebula_attunement'
ORDER BY id;

SELECT '=== VERIFICATION: Summary ===' as section;
SELECT
  skill_id,
  COUNT(*) as record_count,
  SUM(xp_delta) as total_recorded_xp,
  CASE
    WHEN skill_id = 'smuggling' THEN 'Expected: 50 XP (1 mission record)'
    WHEN skill_id = 'nebula_attunement' THEN 'Expected: 60 XP (4 jumps + 1 mission)'
    ELSE 'Unknown'
  END as expected_state
FROM xp_observations
WHERE agent_id = 'craftsman-1'
  AND skill_id IN ('smuggling', 'nebula_attunement')
GROUP BY skill_id
ORDER BY skill_id;

-- ============================================================================
-- STEP 5: Restore from backup if needed (use only if deletion was wrong)
-- ============================================================================

-- To restore from backup, uncomment these lines:
-- DELETE FROM xp_observations WHERE agent_id = 'craftsman-1' AND skill_id IN ('smuggling', 'nebula_attunement');
-- INSERT INTO xp_observations SELECT * FROM xp_observations_backup_20260415;

-- After confirming restoration is successful, you can drop the backup:
-- DROP TABLE xp_observations_backup_20260415;
