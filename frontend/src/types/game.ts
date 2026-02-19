export interface Player {
  username: string;
  ship: string;
  shipClass: string;
  empire: Empire;
  credits: number;
  hull: number;
  hullMax: number;
  shield: number;
  shieldMax: number;
  fuel: number;
  fuelMax: number;
  cargo: number;
  cargoMax: number;
  location: {
    systemId: string;
    system: string;
    poi: string;
    dockedAt: string | null;
  };
  policeLevel: PoliceLevel;
  tick: number;
  traveling?: boolean;
  travelProgress?: number;        // 0.0 to 1.0
  travelDestination?: string;     // target POI id or system id
  travelType?: 'travel' | 'jump'; // travel within system or jump between systems
  travelArrivalTick?: number;     // server tick when travel completes
  travelStartTick?: number;       // server tick when travel started
  skills: Record<string, { level: number; xp: number }>;
}

export interface ShipClass {
  id: string;
  name: string;
  class: string;
  description: string;
  price: number;
  required_skills: Record<string, number>;
  base_hull: number;
  base_shield: number;
  base_armor: number;
  base_fuel: number;
  base_speed: number;
  cargo_capacity: number;
  cpu_capacity: number;
  power_capacity: number;
  defense_slots: number;
  utility_slots: number;
  weapon_slots: number;
  default_modules: string[];
}

export interface OwnedShip {
  id: string;
  class_id: string;
  name: string;
  hull: number;
  max_hull: number;
  shield: number;
  max_shield: number;
  fuel: number;
  max_fuel: number;
  armor: number;
  speed: number;
  cargo_capacity: number;
  cargo_used: number;
  cpu_capacity: number;
  cpu_used: number;
  power_capacity: number;
  power_used: number;
  defense_slots: number;
  utility_slots: number;
  weapon_slots: number;
  modules: string[];
  cargo: { item_id: string; quantity: number }[];
  docked_at_base: string;
}

export type Empire = 'solarian' | 'voidborn' | 'crimson' | 'nebula' | 'outerrim';
export type PoliceLevel = 'lawless' | 'policed';

export interface Skill {
  name: string;
  level: number;
  xp: number;
  nextLevelXp: number;
}

export interface ChatMessage {
  time: string;
  user?: string;
  message: string;
  type?: 'system';
}

export interface Notification {
  type: string;
  icon: string;
  message: string;
  time: string;
}

export interface GalaxySystem {
  id: string;
  name: string;
  x: number;
  y: number;
  empire: Empire;
  online: number;
}

export interface POI {
  id: string;
  type: POIType;
  name: string;
  x: number;
  y: number;
  resources?: Resource[];
}

export type POIType = 'sun' | 'planet' | 'moon' | 'asteroid_belt' | 'asteroid' | 'nebula' | 'gas_cloud' | 'ice_field' | 'relic' | 'station' | 'jump_gate';

export interface Resource {
  name: string;
  amount: number;
}

export interface MarketOrder {
  item: string;
  quantity: number;
  bestBid: number;
  value: number;
}

export interface Recipe {
  name: string;
  category: string;
  skillReq: string;
  input: { name: string; count: number }[];
  output: { name: string; count: number }[];
  canCraft: boolean;
  maxCraftable: number;
}

export interface JumpGate {
  id: string;
  name: string;
  angle: number; // degrees, 0° = North, clockwise
}

export interface Facility {
  id: string;
  name: string;
  category: 'service' | 'infrastructure' | 'production' | 'faction' | 'personal' | 'unknown';
  level: number;
  last_updated: number;
}

export interface Base {
  id: string;
  poi_id: string;
  name: string;
  description: string;
  empire: Empire;
  defense_level: number;
  has_drones: boolean;
  public_access: boolean;
  services: Record<string, boolean>;
  facilities: Facility[];
}
