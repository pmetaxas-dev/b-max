// Toolbar popup: the mini planet, Max's line for today, the first tasks in
// priority order and a button to open B-MAX. "Server unavailable" when needed.

import * as api from '../shared/api.js';
import { button, clear, el } from '../shared/a11y.js';
import { createPlanetView } from '../shared/planet-placeholder.js';

const app = document.getElementById('app');
const planetSlot = document.getElementById('planet-slot');
const planet = createPlanetView('popup');
planetSlot.append(planet.element);

const SHOWN_TASKS = 3;
const openFull = () => chrome.tabs.create({ url: chrome.runtime.getURL('dashboard/dashboard.html') });

/** planetState: the server's PlanetVisualState, or null for the last known one. */
function showPlanet(planetState) {
  planetSlot.hidden = false;
  planet.update(planetState);
}

async function load() {
  clear(app);
  app.append(el('p', { class: 'muted', text: 'Loading…' }));
  let s;
  try {
    s = await api.chatState();
  } catch (err) {
    if (err instanceof api.ServerUnavailableError) return renderUnavailable();
    return renderError(err);
  }
  render(s);
}

function renderUnavailable() {
  clear(app);
  showPlanet(null); // last valid era from the cache; no progress is invented
  app.append(
    el('h1', { text: 'Server unavailable' }),
    el('p', { class: 'muted', text: 'Start the local server and try again.' }),
    button('Try again', load),
  );
}

function renderError(err) {
  clear(app);
  app.append(el('p', { class: 'error', role: 'alert', text: `Something went wrong: ${err.message}` }), button('Try again', load));
}

function render(s) {
  clear(app);
  if (!s.onboarded) {
    planetSlot.hidden = true;
    app.append(
      el('h1', { text: 'B-MAX' }),
      el('p', { text: 'Say hi to Max.' }),
      button('Start', openFull),
    );
    return;
  }

  showPlanet(s.planet);
  app.append(el('h1', { class: 'sr-only', text: 'B-MAX' }));
  if (s.motivation) app.append(el('p', { class: 'step-current', text: s.motivation }));

  const open = s.tasks.filter((x) => !x.doneAt);
  if (open.length) {
    app.append(el('ol', {}, ...open.slice(0, SHOWN_TASKS).map((task) =>
      el('li', {}, task.text, task.needsEstimate ? el('span', { class: 'muted', text: ' (?)' }) : null))));
    if (open.length > SHOWN_TASKS) app.append(el('p', { class: 'muted', text: `+${open.length - SHOWN_TASKS}` }));
  }
  app.append(el('div', { class: 'row', style: 'margin-top:16px' }, button('Open B-MAX', openFull)));
}

load();
