---
name: wildlife-respawn-bloom
description: wildlife regrowth follows a belt's resource depletion; survey_system with a survey_scanner fitted reports a per-system "bloom" value (operator 2026-11-25)
metadata:
  type: reference
---

Wildlife respawn rate is mostly tied to how depleted the belt is: a mined-out belt has no food for wildlife to grow from (operator 2026-11-25).

With a `survey_scanner` module fitted, `survey_system` output includes a wildlife line, e.g. `Wildlife census: 4 species | bloom dormant (0.00)`. The operator thinks bloom is probably linked to respawn rate. The field is NOT returned without the scanner, and hunters (Gauntlets) do not carry one.

**How to apply:** the hunt worker's "empty look keeps hunters off for 1 hour" (`huntEmptyLookFor`, pkg/worker/hunt_delivery.go) is a guess to be tuned; a better rule would use belt depletion (KB poi_resources) or a captured bloom value. Check the real survey_system field name before coding against it.

Related: [[wildlife-loot-drops-vary]], [[hunt-training-data-cutoff]], [[index_combat_wildlife]]

**Measured 2026-11-27 (5,700 looks, 11-22 to 11-27):** what the NEWEST look at a ground found predicts presence; age barely matters. Species seen at the last look: still there 83-93% at every age out to 2 days. Species missing at the last look: there 14% within the hour, 25-36% after that, no clear recovery by 2 days. So `huntClassifyGround` now goes by the newest look (`huntSightedFor = 72h`), not "any sighting within 6h" (that rule gave 11% hits on 438 visits). A need is only accepted for a ground seen at the last look or never looked at; coronid and void_browser had zero such grounds that day (Sol Nacre / Void-Charged Ichor were the top abandons).

**Hunt pass vs the 30-minute command cap:** one hunt pass is one dispatched command, capped by `SleepCommandMaxWait` (30 min). Long-range sorties outlast it; "jump ... context deadline exceeded" exactly 30 min after pass start is this cap, not the server. `commandTimeoutScale` in `pkg/worker/standing.go` gives hunt 4x (2h). "timeout waiting for initial response" is the genuine server timeout.
