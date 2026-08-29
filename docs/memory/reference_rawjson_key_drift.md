---
name: reference_rawjson_key_drift
description: "storeRawJSON keys reachable ONLY via the action switch are silently dead — live replies often omit `action`. Two confirmed instances; audit the rest."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2671530b-761a-4dbe-b378-62f725016c20
  modified: 2026-08-02T04:00:06.834Z
---

**`pkg/game/client.go` `storeRawJSON()` assigns a cache key two ways, and the
two disagree.** First an action-based switch (`resp.Payload["action"]` →
`case "get_system"`, `case "list_ships"`, …), then a series of content-based
fallbacks that claim a payload by shape (`hasItems` → `market`, `hasShips` →
`ships`, `hasPOIs` → `system`) whenever `storeKey` is still empty.

**Many live replies carry NO `action` field.** When that happens the
action-switch case never runs, a content fallback claims the payload under a
DIFFERENT key, and the key the switch was written for stays empty forever.
Nothing errors. `GetRawJSON("that_key")` just returns nil, and any decoder
downstream reports "nothing captured" rather than a failure.

**Two confirmed instances, both silent for months:**
- `browse_ships` → ship listings dead **2026-02-18..2026-07-04**. Fixed by
  giving it a dedicated `ship_listings` key with a shape test. Its code comment
  is the warning that named the pattern.
- `list_ships` → **`owned_ships` was empty from the day it was added**
  (found 2026-08-01, fixed `42116dc`). A live reply is
  `{"active_ship_class":…,"active_ship_id":…,"count":2,"ships":[…]}` — no
  `action` — so it fell through to `hasShips` and landed under `"ships"`.
  `cmd/tools/daily-summary` had papered over it with a read-side fallback to
  `"ships"` instead of fixing the key, so the second consumer
  (`pkg/assets`) inherited the bug and captured zero hulls for every agent.

**⭐ AUDIT THE REST.** Any `case "<cmd>":` in that switch whose live reply
omits `action` is dead the same way. Cheap check: run the command under
`play_as --debug=1 --debug-full-payload=true` and read the
`Stored raw JSON for <key>` line — it prints the key actually used.

**Two lessons that generalise beyond this function:**
- **A read-side fallback at the call site hides the bug for the next
  consumer.** daily-summary's `owned_ships`-then-`ships` fallback worked, so
  nobody fixed the key, so pkg/assets broke. Fix the producer.
- **Never compose a test fixture by hand for a wire shape.** The pkg/assets
  golden fixture *invented* an `"action":"list_ships"` wrapper the server never
  sends — which is exactly why a fully green suite never revealed the dead key.
  Capture a real payload from the debug log instead. See
  [[reference_missions_vacuous_test_trap]] for the same class of failure.

Related: [[project_agent_capability_ledger]] · [[reference_server_docs_sync]]
