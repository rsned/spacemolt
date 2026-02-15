import type { Player, Skill, ChatMessage, Notification, GalaxySystem, POI, MarketOrder, Recipe } from '../types/game';

export const mockPlayer: Player = {
  username: "Atlas 'Astro' Anderson",
  ship: "Deeprock Harvester",
  shipClass: "MINING_CRUISER",
  empire: "solarian",
  credits: 73594,
  hull: 350,
  hullMax: 350,
  shield: 100,
  shieldMax: 100,
  fuel: 26,
  fuelMax: 200,
  cargo: 353,
  cargoMax: 400,
  location: {
    system: "Zibal",
    poi: "Zibal Ice Shelf",
    dockedAt: null,
  },
  policeLevel: "lawless",
  tick: 67996,
};

export const mockSkills: Skill[] = [
  { name: "Mining Basic", level: 9, xp: 90, nextLevelXp: 100 },
  { name: "Mining Advanced", level: 4, xp: 40, nextLevelXp: 100 },
  { name: "Trading", level: 3, xp: 30, nextLevelXp: 100 },
  { name: "Exploration", level: 2, xp: 20, nextLevelXp: 100 },
  { name: "Navigation", level: 2, xp: 20, nextLevelXp: 100 },
  { name: "Small Ships", level: 2, xp: 20, nextLevelXp: 100 },
  { name: "Crafting Basic", level: 1, xp: 10, nextLevelXp: 100 },
  { name: "Fuel Efficiency", level: 1, xp: 10, nextLevelXp: 100 },
  { name: "Refinement", level: 1, xp: 10, nextLevelXp: 100 },
  { name: "Scanning", level: 1, xp: 10, nextLevelXp: 100 },
];

export const mockChat: ChatMessage[] = [
  { time: "12:34", user: "VoidWalker", message: "Anyone mining near here?" },
  { time: "12:35", user: "Atlas 'Astro' Anderson", message: "Just arrived at the ice shelf. Resources look good." },
  { time: "12:36", type: "system", message: "Player CrimsonBlade entered" },
];

export const mockNotifications: Notification[] = [
  { type: "combat", icon: "⚔", message: "Pirate spotted!", time: "3s" },
  { type: "skill", icon: "📈", message: "Mining Basic → 9", time: "1m" },
  { type: "trade", icon: "💰", message: "Sold 50 iron for 250cr", time: "2m" },
  { type: "mission", icon: "📋", message: "Delivery complete", time: "5m" },
];

export const mockGalaxySystems: GalaxySystem[] = [
  { id: "zibal", name: "Zibal", x: 100, y: 150, empire: "solarian", online: 3 },
  { id: "frontier", name: "Frontier", x: 200, y: 100, empire: "voidborn", online: 1 },
  { id: "alpha", name: "Alpha Station", x: 150, y: 200, empire: "crimson", online: 0 },
  { id: "nebula", name: "Nebula Prime", x: 250, y: 180, empire: "nebula", online: 5 },
  { id: "outerrim", name: "Rim Outpost", x: 80, y: 250, empire: "outerrim", online: 2 },
];

export const mockSystemPOIs: POI[] = [
  { id: "star", type: "sun", name: "Zibal Star", x: 0, y: 0 },
  { id: "planet1", type: "planet", name: "Zibal I", x: 1, y: 0 },
  { id: "planet2", type: "planet", name: "Zibal II", x: 2, y: 0.3 },
  { id: "planet3", type: "planet", name: "Zibal III", x: 3, y: 0.3 },
  {
    id: "ice",
    type: "ice_field",
    name: "Zibal Ice Shelf",
    x: 4.2,
    y: -1.6,
    resources: [
      { name: "Water Ice", amount: 62 },
      { name: "Nitrogen Ice", amount: 35 },
      { name: "Helium Ice", amount: 15 },
    ],
  },
];

// Connected systems with jump gate data (angle in degrees, 0° = North, clockwise)
export const mockJumpGates = [
  { id: "sys_0455", name: "GSC-0009", angle: 203.7 },
  { id: "sys_0418", name: "Keelbreak", angle: 298.1 },
  { id: "sys_0048", name: "Revati", angle: 26.6 },
];

export const mockMarketOrders: MarketOrder[] = [
  { item: "ore_iron", quantity: 45, bestBid: 5, value: 225 },
  { item: "ore_copper", quantity: 12, bestBid: 8, value: 96 },
  { item: "ore_ice_water", quantity: 156, bestBid: 3, value: 468 },
];

export const mockRecipes: Recipe[] = [
  {
    name: "Refine Steel",
    category: "Refining",
    skillReq: "refinement ≥1",
    input: [{ name: "ore_iron", count: 5 }],
    output: [{ name: "refined_steel", count: 2 }],
    canCraft: true,
    maxCraftable: 9,
  },
];
