---
name: project_modulated_laser_chain
description: "Modulated mining laser chain approved 2026-11-22: magnet sinter + rhodium optics at CJ (placed), modulated_mining_laser_line at Beid (mats hauling via craftsman-2 tasks 28-30); station focus CJ=refined/components, Beid=modules/weapons/ammo"
metadata:
  node_type: memory
  type: project
  originSessionId: 8c15f693-8825-4572-8f22-51141db1e328
  modified: 2026-11-22T19:54:10.856Z
---

**Operator direction 2026-11-22:** CJ production focuses on refined goods + components for modules/ships; Beid focuses on modules, weapons (flak_cannon, null_cannon) and ammo. First approved build: the modulated mining laser chain (laser = mining power 18, works thin/depleted deposits; for our own mining fleets).

**Chain:** build_modulated_mining_laser = 1 mining_laser_ii + 2 neodymium_magnet + 1 processing_core + 1 rhodium_optic_array.
- neodymium_magnet_sinter at CJ, facility b5d86504ef4f4f386a305bfce7033054 (sinter_neodymium_magnet: 2 neodymium_ore + 2 iron_ore + 1 purified_argon).
- rhodium_optics_workshop at CJ, facility 84c6fa94bce2726f41c5cf06cece8cd1 (2 reinforced_glass + 1 focused_crystal + 1 rhodium_coating -> 2 arrays).
- modulated_mining_laser_line at Beid: placed 2026-11-22 12:20 server time by craftsman-2, facility c8298e7ee1224f4b9e58344703d75549. Haul tasks 28-30 done and removed. Operator is moving craftsman-1's mining_laser_ii + processing_core to Beid faction storage.
- Both CJ builds placed by miningbot_0000 11:53 server time; treasury was 2.3M before (floor 1M).

**Input holders (11-22):** neodymium_ore marketbot_011 50k + marketbot_barnard_44 23.5k (Sable Port); rhodium_ore marketbot_015 43k (Ironhearth); rhodium_coating 262 in CJ faction (needs rhodium_deposition_chamber for more, short chemical_liner); reinforced_glass ~31k across many agents (Central Nexus / Grand Exchange); mining_laser_ii and most processing_core are craftsman-1's (operator's account: ask, never touch).

**Deferred:** barrel_assembly + tracking_system at CJ gate every weapon facility; flak yard after; null cannon limited by void inputs (224 voidborn_null_matrix). Pathfinder drives: no more for sale; rentable chain at Confederacy Central Command; JDC (exotic_crystal) is the gate; operator asked how many — build-own at Beid wins past ~50. See [[reference_pathfinder_drive_sourcing]].

Related: [[project_cloverfield_home_station]] [[project-station-two-beid]]

**Feeds live 2026-11-22 ~12:52 server time:** operator deposited 130 mining_laser_ii + 20 processing_core in Beid faction storage. marketbot_barnard_44 gifted 2,000 neodymium_ore to craftsman-2 at Sable Port. craftsman-2 tasks: 31 + 32 are ONE-OFF hourly delivers (neodymium_ore Sable Port -> CJ faction; its own 772 reinforced_glass Grand Exchange -> CJ faction): REMOVE after landing. 33/34 daily magnet 200 / optic array 100 CJ faction -> Beid faction; 35 hourly `craft build_modulated_mining_laser 10`. miningbot_0000 65 hourly `craft sinter_neodymium_magnet 20`, 66 hourly `craft assemble_rhodium_optic_array 10`. Processing cores (20) are the next limit.
