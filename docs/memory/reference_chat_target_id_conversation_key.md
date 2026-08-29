---
name: reference_chat_target_id_conversation_key
description: "A private chat_message's target_id is now a conversation key \"<recipient>:<sender>\", not a bare recipient id — it silently killed databot for two months"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-10T17:51:24.075Z
---

**`chat_message.target_id` on the `private` channel is a CONVERSATION KEY, not a
recipient id.** Observed live 2026-08-10:

```
Response Type: 'chat_message'
  {"channel":"private","sender_id":"a5092491…",
   "target_id":"0e72a09d…:a5092491…"}    ← <recipient>:<sender>
```

It used to be the bare recipient player id. Any `target_id == myPlayerID`
check therefore rejects **every inbound DM**.

**It killed databot from 2026-06-12 to 2026-08-10 and nothing alerted.** Two
reasons the outage was invisible, both worth generalising:
1. **The skip path marked the dropped message READ** (`58acbce3`, added to stop
   a drain loop). Every discarded request was consumed on its way past, so no
   backlog ever built up to signal trouble. A filter that consumes what it
   rejects destroys its own evidence — log it or leave it unread.
2. **Every test fixture still used the old bare shape**, so all six tests in
   `pkg/dataservice` passed straight through the outage. Same trap as
   [[reference_pirates_standing_key_drift]]: fixtures frozen at the old wire
   shape keep a suite green while production is dead.

**FIXED** in `pkg/dataservice/service.go` — `addressedTo(targetID, agentID)`
splits on `:` and compares segments **whole** (a prefix must not match), and
accepts **either** side because the pairing order is not guaranteed. Matching
both sides is only safe because the from-self check immediately below rejects
our own messages, whose keys also contain our id — do not remove one without
the other.

**Audit note:** any other reader of `target_id` has the same bug. `pkg/mbox`
stores it verbatim (fine), but check any consumer that compares it. Sending is
unaffected — outbound `target_id` is still a plain recipient id.

Related: [[project_kind_discriminator_drift]] (the other silent v0.5xx wire
change) · [[reference_server_docs_sync]].
