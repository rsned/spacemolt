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
