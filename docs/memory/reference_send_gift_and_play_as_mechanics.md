---
name: reference_send_gift_and_play_as_mechanics
description: "Operational mechanics for moving credits between agents: send_gift requires a DOCKED sender and a game USERNAME (not agent_id), autopilot does not dock, and play_as never exits on stdin EOF"
metadata: 
  node_type: memory
  type: reference
  originSessionId: f744e650-ff1a-4add-9401-5a3087024568
  modified: 2026-07-30T03:49:48.349Z
---

Learned 2026-07-29 doing a 14-agent, 1.4M credit bailout from johnny_cab. Every one of these cost a failed attempt.

**`send_gift` payload is `{recipient, credits, message}`** — see `pkg/worker/rescue_fee.go:44` and `deliver.go:291`. CLI: `send_gift "<recipient>" credits <amount>` in play_as, or `server-cmd --agent=<id> --cmd=send_gift --payload recipient=... --payload credits=...`.

**`recipient` is the GAME USERNAME, not the agent_id.** `deliver.go` takes `(recipient, username string)` and passes `username`. Usernames live in `data/agents/<id>/credentials.json` and are nothing like the agent id — `explorer-4` is `Cosmo 'Cosmic' Chandler`, `random-3` is `root`, `random-9` is `🤘`. Several look truncated at ~24 chars (`Blade 'Battler' Blackwoo`); use them verbatim. **Generate the command list programmatically from the credentials files** rather than transcribing 14 of those by hand.

**The SENDER must be DOCKED.** All 14 gifts failed `not_docked: You must be docked at a station to perform this action` and moved ZERO credits — no partial state to unwind. `rescue_fee.go` gates on `st.Doc` for the same reason.

**⭐ THE RECIPIENT NEED NOT BE PRESENT — a gift is deposited AT THE SENDER'S STATION and waits in the recipient's storage.** Only the sender's location matters. Evidence: the 14 credit gifts landed on agents scattered across Nova Terra / Sirius / Procyon / Sol / Alpha Centauri while johnny_cab sat at `the_experiment`; and 2× `refueling_pump` gifted at Nexus Prime showed up in `view_storage` for a recipient that was docked but never involved. The wire shape says so too — `SendGiftResponse` carries a **required `base_id`** plus `storage_remaining`, and there is a distinct `StorageGift` shape (items/ships/credits + sender). So the only real constraint is **"gift at a station the recipient can reach."** Any note claiming co-location is required is wrong. Full item form: `send_gift(recipient, credits?, item_id?, message?, quantity?, ship_id?, source?)`.

**An ITEM gift is not usable on arrival.** It sits in *storage*, so the recipient needs `withdraw_items <item> <qty>` to get it into cargo, and a module additionally needs `install_mod <module_id>`. Worker loops generally do none of this — plan on doing it by hand.

**`base_id` in the receipt is a BASE id, not a POI id, and the two differ.** `central_nexus` is the base at poi `the_core`; `grand_exchange_station` is the base at poi `grand_exchange`. A receipt naming an unfamiliar station is usually the base alias for the poi you were already at — check `select id, poi_id from bases where id=?` before concluding the gift went somewhere else and dispatching a ship.

**🔴 `autopilot <system> <poi>` TRAVELS TO the POI but does NOT DOCK.** The prompt shows `(🚀Space)` afterwards. An explicit `dock` is required. This is what caused the failed round above.

**🔴 `play_as` NEVER EXITS ON STDIN EOF** — it spins forever printing `Error reading input: EOF`, so a piped script hits your timeout and looks like a hang. **End every script with `quit`.** One play_as session handles many commands, which beats `server-cmd`'s one-fresh-login-per-invocation when doing 14 of anything (and avoids the per-IP /login limit).

**`worker.SplitArgs` handles quoting and preserves inner apostrophes**, so `send_gift "Cosmo 'Cosmic' Chandler" credits 100000` parses correctly — it treats both `"` and `'` as quote chars but writes everything verbatim until the matching close quote.

**Using play_as on a SUPERVISED agent needs its worker stopped first**, or you get `session_replaced` thrash. Stopping the whole overmind for a single-worker fleet (shuttle = johnny_cab alone) is cleaner than the SIGSTOP dance, because it removes the 90s `SilenceTimeout` pressure entirely and 14 gifts take ~10s each. See [[feedback_play_as_go_run]].

**Verify from the SENDER's stats, not the recipients'**: `credits_sent` per response, plus wallet and "Credits gifted"/"Gifts sent" deltas in `get_status`. A recipient worker's `credits` in the overmind status file reports its **cached** `State.Credits` and lags badly — a gifted worker can read 0 for many minutes. A fresh login does re-read it (confirmed: restarted fighter-3 came back reading 100,000).

Cached position also lags: the status file had johnny_cab at `GSC-0041` (a station-less system) when it was actually already in `the_experiment`. **Check live `get_status` before planning travel off a status-file position.**
