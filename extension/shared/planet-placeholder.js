import { el } from './a11y.js';
import { ERA_LABELS, PLANET_PAGE, planetQuery, resolvePlanetState } from './planet-state.js';

// The planet wrapper. The planet itself (shared/planet/planet.html) is the
// self-contained Three.js runtime, declared as a sandboxed page in the
// manifest because it uses inline scripts. It never talks to Go: this
// wrapper takes the server's PlanetVisualState
//   { progress: 0..1, era, eraLabel, weather, ... }
// validates it, and opens the planet once with ?era=&weather=&view=.
// Text always sits beside the visual: never colour or motion alone (§12).
//
// A view is created once per page and kept outside the re-rendered content,
// so re-rendering after a step confirmation does not reload the 18MB scene;
// the iframe is reloaded only when the era or weather actually changes.
export function createPlanetView(view) {
  const frame = el('iframe', { class: `planet-frame planet-frame-${view}`, title: 'Planet' });
  const caption = el('figcaption', {});
  const element = el('figure', { class: 'planet' }, frame, caption);
  let shownUrl = '';
  let generation = 0;

  /** planet: the server's PlanetVisualState, or null when Go is unavailable. */
  async function update(planet) {
    const mine = ++generation;
    const state = await resolvePlanetState(planet, globalThis.chrome?.storage?.local);
    if (mine !== generation) return; // a newer update already won
    const label = ERA_LABELS[state.era];
    const text = state.source === 'server'
      ? `Planet: ${Math.round((planet.progress ?? 0) * 100)}% progress — ${label}`
      : state.source === 'cache'
        ? `Planet: last known era — ${label}`
        : `Planet — ${label}`;
    caption.textContent = text;
    frame.title = planet?.description ? `${text}. ${planet.description}` : text;
    const url = `${chrome.runtime.getURL(PLANET_PAGE)}?${planetQuery(state, view)}`;
    if (url !== shownUrl) {
      shownUrl = url;
      frame.src = url;
    }
  }

  return { element, update };
}
