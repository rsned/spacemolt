package market

import (
	"context"
	"database/sql"
	"time"
)

// MissionPayoutRatioMinSamples is how many recent completions the ratio needs
// before it is trusted enough to discount a reward.
//
// Below it the ratio reports 1.0 — no discount. That default is deliberately
// the OPTIMISTIC one, and it is what lets the gate recover on its own: if the
// discount ever bites hard enough that the fleet stops taking missions, the
// samples age out of the window, the ratio reverts to 1.0, missions get taken
// again, and the fresh results either re-tighten the gate or reveal that the
// economy has healed. A pessimistic default would latch the fleet off missions
// permanently with no way to observe a recovery.
const MissionPayoutRatioMinSamples = 8

// MissionPayoutRatio returns realized credits divided by advertised credits
// across completed missions finished within window, plus the sample count.
//
// Context: on 2026-07-23 the empire treasury stopped being replenished, and
// mission payouts decayed from 100% of advertised to ~37% over ten days —
// paying partial or zero credits while awarding the full advertised skill XP.
// The gate scores candidates on advertised rewards, so without this it flies
// 27-jump runs priced at 2000 that settle for 370.
//
// Rows with expected_reward <= 0 are excluded: a resumed mission recorded 0
// before the resume-reward fix, and dividing by it would be meaningless.
// Abandoned and failed rows are excluded too — this measures what the empire
// PAYS on delivery, not whether the fleet finishes what it starts.
func (c *Collector) MissionPayoutRatio(ctx context.Context, window time.Duration) (float64, int, error) {
	cutoff := time.Now().UTC().Add(-window).Format("2006-01-02T15:04:05Z")

	var (
		promised sql.NullFloat64
		paid     sql.NullFloat64
		n        int
	)
	err := c.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(expected_reward), 0), COALESCE(SUM(credits_earned), 0), COUNT(*)
  FROM mission_results
 WHERE outcome = 'completed'
   AND expected_reward > 0
   AND finished_at > ?`, cutoff).Scan(&promised, &paid, &n)
	if err != nil {
		return 1, 0, err
	}
	if n < MissionPayoutRatioMinSamples || !promised.Valid || promised.Float64 <= 0 {
		return 1, n, nil
	}

	ratio := paid.Float64 / promised.Float64
	// Clamp: a ratio above 1 (a bonus-paying window) must not inflate rewards
	// into taking runs the advertised price would not justify, and a negative
	// is not physically meaningful.
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}

	return ratio, n, nil
}
