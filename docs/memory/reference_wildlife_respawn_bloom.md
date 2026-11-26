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
