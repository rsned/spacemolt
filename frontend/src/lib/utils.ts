import type { Empire, POIType } from '../types/game';

export const getEmpireColor = (empire: Empire): string => {
  const colors: Record<Empire, string> = {
    solarian: '#eab308',
    voidborn: '#a855f7',
    crimson: '#dc2626',
    nebula: '#14b8a6',
    outerrim: '#22c55e',
  };
  return colors[empire] || '#6b7280';
};

export const getEmpireTextColor = (empire: Empire): string => {
  const colors: Record<Empire, string> = {
    solarian: 'text-yellow-500',
    voidborn: 'text-purple-500',
    crimson: 'text-red-500',
    nebula: 'text-teal-500',
    outerrim: 'text-green-500',
  };
  return colors[empire] || 'text-gray-500';
};

export const getPOIIcon = (type: POIType): string => {
  const icons: Record<POIType, string> = {
    sun: '☀',
    planet: '🪐',
    moon: '🌙',
    ice_field: '❄',
    asteroid_belt: '◆',
    asteroid: '◆',
    nebula: '☁',
    gas_cloud: '☁',
    relic: '◎',
    station: '⬡',
    jump_gate: '⊕',
  };
  return icons[type] || '•';
};

export const getPOIColor = (type: POIType): string => {
  const colors: Record<POIType, string> = {
    sun: 'text-yellow-400',
    planet: 'text-blue-400',
    moon: 'text-gray-400',
    ice_field: 'text-cyan-300',
    asteroid_belt: 'text-amber-600',
    asteroid: 'text-amber-600',
    nebula: 'text-purple-400',
    gas_cloud: 'text-teal-400',
    relic: 'text-yellow-500',
    station: 'text-white',
    jump_gate: 'text-cyan-400',
  };
  return colors[type] || 'text-gray-500';
};

export const getBarColor = (pct: number, type: 'hull' | 'fuel' | 'shield'): string => {
  if (pct > 60) return 'bg-green-500';
  if (pct > 30) return 'bg-yellow-500';
  return type === 'hull' ? 'bg-red-500' : 'bg-orange-500';
};
