---
name: notifications-tick-tracking
description: get_notifications used for lightweight tick refresh after login and periodically in runner loop
type: project
---

Added get_notifications command on 2026-03-19 for lightweight tick/timestamp tracking.

**Why:** get_status is heavyweight (returns full state), while get_notifications just returns current_tick, timestamp, and pending notifications. Reduces unnecessary server load.

**How to apply:**
- Called immediately after login (in manager, observer, MCP bridge) to seed initial tick
- Called by runner when tick hasn't advanced, before deciding to skip the cycle
- State.ServerTimestamp tracks the server's UNIX time from this call
- Added as MCP bridge tool for manual use
