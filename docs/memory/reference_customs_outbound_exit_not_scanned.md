---
name: reference-customs-outbound-exit-not-scanned
description: Customs scans ships ENTERING an empire, not leaving it; contraband routing allows a monitored system only as an outbound exit (operator 2026-11-24)
metadata:
  type: reference
---
Customs inspects arrivals entering an empire; ships leaving one are not scanned (operator, 2026-11-24).

Since commit 79e28ecb, contraband routing (Autopilot + mission pre-accept check, `contrabandGate` in pkg/worker/mission_contraband_route.go) lets a ship pass through a customs-monitored system only when it arrives from that empire AND leaves to a system outside it. Entry from outside and internal transit (same empire both sides) are still avoided. Unknown empire = never an exit. Our customs map has no arrival-direction data, so this rule is operator-stated, not measured.

Example: Treasure Cache (Nebula) -> Pollux -> Nusakan/Sadalmelik -> ... -> Alhena (an_introduction delivery) is 5 jumps; before this, pirate-7/8/9 sat ~1h unable to depart. Side effect: Treasure Cache -> Sol now exits via The Levy (17 jumps).

Related: [[feedback-stronghold-routing-requires-pirate-unlock]]
