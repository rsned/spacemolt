---
name: reference_empire_tax_day
description: "Weekly empire tax day deducts credits from agents (property/sales/etc); get_tax_estimate shows amount + reason. Confound for agent P&L — a credit drop isn't necessarily a strand/overpay."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
---

There is a **weekly empire tax day** on which the server deducts credits from every
agent (basis: property, sales, etc.). The in-game command **`get_tax_estimate`** shows
how much will be/was taken and why.

**Why it matters:** when reconciling agent profit/loss, a credit drop on tax day can be
tax, not a trading loss / fuel-strand / surge-overpay. Do NOT misattribute it. Cross-check
`get_tax_estimate` (and the action log — [[project_action_log_analyzer]]) before diagnosing
a balance drop as a stranding incident.

Relevant to mission-learning-pool P&L ([[project_mission_learning_pool]]) and any fleet
earnings dashboard ([[project_fleet_efficiency_dash]]). One observed tax day: 2026-07-19.
