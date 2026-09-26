import * as api from './api.js';
import { button, el } from './a11y.js';
import { t } from './i18n.js';
import { searchUrl } from './search.js';

// "Where to start": the step's three searches (phrases from the server; the
// link is built here) under the cue. Opens a normal tab.
function searchButtons(queries, lang, open) {
  const links = (queries ?? []).map((q) => [q, searchUrl(q)]).filter(([, url]) => url);
  if (!links.length) return null;
  return el('div', { class: 'row start-searches' },
    el('p', { class: 'muted', text: t(lang).whereStartTitle }),
    ...links.map(([q, url]) => button(`🔍 ${q}`, () => open(url), { class: 'secondary' })));
}

// Local presentation only. Go supplies the cue and validates the parent step.
export function createStartingCue({ onError, onStepChanged, openTab = (url) => chrome.tabs.create({ url }) }) {
  let generation = 0;
  let timer;
  let text;
  let searches;

  function clear() {
    generation++;
    clearInterval(timer);
    timer = undefined;
    if (text) text.textContent = '';
    searches?.remove?.();
    searches = undefined;
  }

  function render(step) {
    clear();
    if (!step) return null;
    const version = generation;
    text = el('p', { class: 'muted', 'aria-live': 'polite' });
    const output = text;
    let checking = false;
    const action = button('Give me a smaller start', async () => {
      action.disabled = true;
      try {
        const cue = await api.startCue(step.id);
        if (version !== generation) return;
        if (cue.stepId !== step.id) return;
        output.textContent = cue.text;
        searches?.remove?.();
        searches = searchButtons(cue.searchQueries, cue.lang, openTab);
        if (searches) wrapper.append(searches);
        // An open full tab can outlive a completion in another surface.
        if (timer === undefined) timer = setInterval(async () => {
          if (checking) return;
          checking = true;
          try {
            const summary = await api.getSummary();
            if (version !== generation) return;
            if (summary.currentStep?.id !== step.id) {
              clear();
              onStepChanged();
            }
          } catch (err) {
            if (version !== generation) return;
            clear();
            onError(err);
          } finally {
            checking = false;
          }
        }, 2000);
      } catch (err) {
        if (version !== generation) return;
        clear();
        if (err instanceof api.ApiError && err.status === 409) onStepChanged();
        else onError(err);
      } finally {
        action.disabled = false;
      }
    }, { class: 'secondary' });
    const wrapper = el('div', {}, action, output);
    return wrapper;
  }

  return { render, clear };
}
