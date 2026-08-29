---
name: reference_craft_action_result_wrapping
description: "Craft is a next-tick action: the job body (job_id) arrives in an action_result frame {command,tick,result:{...}}, NOT in the immediate ok. Decoders must unwrap `result` first."
metadata: 
  node_type: memory
  type: reference
  originSessionId: c80d54b4-4ed6-42da-b9a0-488c730ee085
---

**Craft replies are wrapped.** Since the crafting overhaul (server ~v0.485), `craft` behaves like every other next-tick action:

1. The immediate reply is a **pending ok** — `"OK: Action 'craft' pending. Will execute on next tick."` — carrying **no `job_id`**.
2. The job body arrives on the next tick in an **`action_result`**, whose payload is `{"command":"craft","tick":N,"result":{...job body...}}`.

`game.Client.storeRawJSON` caches the *payload* under `"_last"`, so anything reading `GetRawJSON("_last")` after a craft sees **the wrapper, not the job body**. `job_id` / `jobs` / `results` all sit one level down under `result`.

**The trap:** decoding the wrapper into `serverapi.CraftJobQueued` *succeeds* — the fields are simply absent — so this fails as an empty `job_id`, never as a decode error. Symptom: `craft_node: craft <recipe> xN: queue response carried no job_id`.

Always `unwrapActionResult(raw)` before decoding a craft reply (returns `raw` unchanged when there's no `result` key — not every craft reply is wrapped). Implementations: `cmd/tools/play_as/main.go` (learned this during the overhaul) and `pkg/worker/craft_node.go` (fixed 2026-07-12, commit `7038be6`).

**`pkg/game/crafting.go` still carries the stale v0.389 comment** — "the server replies with a single non-pending ok carrying the job body; there is no action_result". That is now false. The `terminateOnActionOrOK` terminator is still correct (it ignores a `pending:true` ok and waits for the action_result), but do not trust that comment.

**Why the tests didn't catch it:** `pkg/worker/craft_node_test.go` fixtures were *bare* job bodies, encoding the same stale assumption as the code — green suite, broken production. When fixing an API-drift bug, check whether the test fixtures are themselves the stale artifact.

**Masking effect:** the craft still queues and completes server-side. `CraftOutputs`' recompute-remaining pass re-reads live storage on retry, so a node eventually stumbles to done via "already have N of N" — but it burns retries, and a retry can fail `have_inputs=false` against its own orphaned job. Looks flaky; isn't.

Part of the unaudited v0.473→v0.485 drift — see [[project_server_restart_warning_event]] and [[project_api_struct_drift_audit]].
