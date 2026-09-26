// Planet runtime URL contract (Planet Runtime Integration Interface).
// Go owns the era and weather; this module only validates what Go sent,
// remembers the last valid snapshot, and builds the sandboxed planet URL.
// No DOM and no chrome.* globals: storage is passed in, so it is unit-tested.

export const ERAS = ['prehistoric', 'copper', 'medieval', 'industrial', 'space'];
export const ERA_LABELS = { prehistoric: 'Prehistoric', copper: 'Copper', medieval: 'Medieval', industrial: 'Industrial', space: 'Space' };
export const WEATHERS = ['clear', 'storm'];
export const VIEWS = ['popup', 'full'];

export const PLANET_PAGE = 'shared/planet/planet.html';
export const DEFAULT_PLANET_STATE = Object.freeze({ era: 'prehistoric', weather: 'clear' });
const CACHE_KEY = 'planetState';

/** Returns { era, weather } when both are contract values, otherwise null. */
export function validPlanetState(value) {
  if (!value || !ERAS.includes(value.era) || !WEATHERS.includes(value.weather)) return null;
  return { era: value.era, weather: value.weather };
}

/**
 * Picks the snapshot to open the planet with. A valid Go state is cached and
 * used; otherwise (Go unavailable or invalid values) the last valid cached
 * state, and on a first run with nothing cached, prehistoric/clear.
 * `source` is 'server', 'cache' or 'default'.
 */
export async function resolvePlanetState(fromServer, storage) {
  const valid = validPlanetState(fromServer);
  if (valid) {
    try {
      await storage?.set({ [CACHE_KEY]: valid });
    } catch {
      /* the planet still opens; only the offline fallback is stale */
    }
    return { ...valid, source: 'server' };
  }
  let cached = null;
  try {
    cached = validPlanetState((await storage?.get(CACHE_KEY))?.[CACHE_KEY]);
  } catch {
    /* fall through to the default */
  }
  return cached ? { ...cached, source: 'cache' } : { ...DEFAULT_PLANET_STATE, source: 'default' };
}

/** Always sends all three parameters, so every opening is deterministic. */
export function planetQuery({ era, weather }, view) {
  if (!VIEWS.includes(view)) throw new Error(`unknown planet view: ${view}`);
  return new URLSearchParams({ era, weather, view }).toString();
}
