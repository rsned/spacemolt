import { useEffect, useState, useRef } from 'react';

export interface CatalogItemDef {
  id: string;
  name: string;
  description: string;
  category: string;
  rarity: string;
  size: number;
  base_value: number;
  stackable: boolean;
  tradeable: boolean;
}

interface CatalogCache {
  items: Record<string, CatalogItemDef>;
  loading: boolean;
  error: string | null;
}

// Global cache for catalog items
let globalCatalogCache: Record<string, CatalogItemDef> = {};
let globalCatalogLoading = false;
let globalCatalogError: string | null = null;
let globalCatalogPromise: Promise<void> | null = null;

/**
 * Fetches catalog item definitions from the knowledge base API.
 * Results are cached globally so multiple components can share the same data.
 */
export function useCatalogItems(): CatalogCache {
  const [state, setState] = useState<CatalogCache>({
    items: globalCatalogCache,
    loading: globalCatalogLoading,
    error: globalCatalogError,
  });

  const initializedRef = useRef(false);

  useEffect(() => {
    if (initializedRef.current) return;
    initializedRef.current = true;

    // If already loading or loaded, don't fetch again
    if (globalCatalogPromise || Object.keys(globalCatalogCache).length > 0) {
      if (Object.keys(globalCatalogCache).length > 0) {
        setState({
          items: globalCatalogCache,
          loading: false,
          error: null,
        });
      }
      return;
    }

    globalCatalogLoading = true;
    setState(s => ({ ...s, loading: true }));

    globalCatalogPromise = fetch('/api/catalog/items')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<CatalogItemDef[]>;
      })
      .then((items) => {
        globalCatalogCache = {};
        for (const item of items) {
          globalCatalogCache[item.id] = item;
        }
        globalCatalogLoading = false;
        globalCatalogError = null;
        setState({
          items: globalCatalogCache,
          loading: false,
          error: null,
        });
      })
      .catch((err) => {
        console.error('Failed to fetch catalog items:', err);
        globalCatalogLoading = false;
        globalCatalogError = err instanceof Error ? err.message : String(err);
        setState({
          items: {},
          loading: false,
          error: globalCatalogError,
        });
      });
  }, []);

  return state;
}
