---
name: reference_mbox_backfill_holes
description: mbox backfill stopped at the first already-known message and never crawled private by default — a June DM was unreachable for 3 months; both FIXED 2026-08-30
metadata:
  type: reference
---

**Symptom (2026-08-30):** `mbox search sinter` found nothing although the
Sinter (Starfall Salvage) NPC DM of 2026-06-06 showed in a manual
`chat_history private`. The message was simply not in the mbox.

**Three feeds, three gaps:** live pushes need a logged-in session (a hand-
flown pilot is mostly parked); the chat poller only surfaces messages newer
than session start; `mbox backfill` defaulted to `system,local,faction` —
`private` had NO cursor row ever. Manual `chat_history` is a pass-through;
it ingests nothing.

**Real bug:** `pkg/mbox/ingest.go` stopped the crawl at the first message
already in the store ("everything below is known") — false whenever pushes
left holes. Page 1 hit an August push, quit before June; the cursor only
advanced on inserted rows so it pinned at 08-26 and every later run re-hit
the next push. `-f/--reset` cannot help (fresh start = same state).

**Fixed `9dde9014` + `669bc6a0`:** known rows are skipped; the crawl ends
on an empty page, the cap (counted over rows SEEN), or a no-progress page;
the cursor is the true crawl floor. `private` is in the default backfill
set. Remedy for any agent with a long-parked pilot: run `mbox backfill`
once (500 rows/channel per run; repeat to go deeper).

See [[reference_chat_target_id_conversation_key]] for the other way private
chat silently vanished.
