import { useEffect, useState, useRef } from 'react';

export interface SkillDefinition {
  id: string;
  name: string;
  category: string;
  description: string;
  max_level: number;
  xp_per_level: number[];
}

interface SkillsCache {
  skills: Record<string, SkillDefinition>;
  loading: boolean;
  error: string | null;
}

// Global cache for skill definitions
let globalSkillsCache: Record<string, SkillDefinition> = {};
let globalSkillsLoading = false;
let globalSkillsError: string | null = null;
let globalSkillsPromise: Promise<void> | null = null;

/**
 * Fetches skill definitions from the knowledge base API.
 * Results are cached globally so multiple components can share the same data.
 */
export function useSkillDefinitions(): SkillsCache {
  const [state, setState] = useState<SkillsCache>({
    skills: globalSkillsCache,
    loading: globalSkillsLoading,
    error: globalSkillsError,
  });

  const initializedRef = useRef(false);

  useEffect(() => {
    if (initializedRef.current) return;
    initializedRef.current = true;

    // If already loading or loaded, don't fetch again
    if (globalSkillsPromise || Object.keys(globalSkillsCache).length > 0) {
      if (Object.keys(globalSkillsCache).length > 0) {
        setState({
          skills: globalSkillsCache,
          loading: false,
          error: null,
        });
      }
      return;
    }

    globalSkillsLoading = true;
    setState(s => ({ ...s, loading: true }));

    globalSkillsPromise = fetch('/api/skills')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<SkillDefinition[]>;
      })
      .then((skills) => {
        globalSkillsCache = {};
        for (const skill of skills) {
          globalSkillsCache[skill.id] = skill;
        }
        globalSkillsLoading = false;
        globalSkillsError = null;
        setState({
          skills: globalSkillsCache,
          loading: false,
          error: null,
        });
      })
      .catch((err) => {
        console.error('Failed to fetch skill definitions:', err);
        globalSkillsLoading = false;
        globalSkillsError = err instanceof Error ? err.message : String(err);
        setState({
          skills: {},
          loading: false,
          error: globalSkillsError,
        });
      });
  }, []);

  return state;
}

/**
 * Get XP required for a specific level of a skill
 */
export function getXPForLevel(skillId: string, level: number, skills: Record<string, SkillDefinition>): number {
  const skill = skills[skillId];
  if (!skill || level < 1 || level > skill.max_level) {
    return 0;
  }
  // level is 1-indexed, array is 0-indexed
  return skill.xp_per_level[level - 1] || 0;
}

/**
 * Get XP required for the next level
 */
export function getNextLevelXP(skillId: string, currentLevel: number, skills: Record<string, SkillDefinition>): number {
  const skill = skills[skillId];
  if (!skill || currentLevel >= skill.max_level) {
    return 0;
  }
  return getXPForLevel(skillId, currentLevel + 1, skills);
}
