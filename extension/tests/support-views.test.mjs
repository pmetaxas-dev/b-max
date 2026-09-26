import assert from 'node:assert/strict';
import test, { beforeEach } from 'node:test';
import { daySummarySection, treasureSection, voiceSection } from '../shared/support-views.js';

beforeEach(() => {
  globalThis.document = {
    createElement: (tag) => ({
      tagName: tag.toUpperCase(), textContent: '', children: [], attrs: {}, listeners: {},
      setAttribute(k, v) { this.attrs[k] = v; },
      addEventListener(type, fn) { this.listeners[type] = fn; },
      append(...kids) { this.children.push(...kids); },
    }),
  };
});

const walk = (node) => [node, ...(node?.children ?? []).flatMap(walk)];
const byTag = (node, tag) => walk(node).filter((n) => n.tagName === tag);

test('the treasure lists unseen ideas, never tasks, and folds the seen ones', () => {
  let seen = 0;
  const node = treasureSection([
    { id: '1', type: 'idea', text: 'desk setup video' },
    { id: '2', type: 'idea', text: 'editing series' },
    { id: '3', type: 'task', text: 'email the sponsor' },
    { id: '4', type: 'idea', text: 'old idea', reviewed: true },
  ], () => seen++, { lang: 'en' });
  assert.equal(byTag(node, 'H2')[0].textContent, '💡 Ideas treasure (2 ideas)');
  const lists = byTag(node, 'UL');
  assert.deepEqual(lists[0].children.map((li) => li.textContent), ['desk setup video', 'editing series']);
  assert.deepEqual(lists[1].children.map((li) => li.textContent), ['old idea']);
  byTag(node, 'BUTTON')[0].listeners.click();
  assert.equal(seen, 1);
});

test('an empty treasure says where ideas come from', () => {
  const node = treasureSection([], () => {}, { lang: 'el' });
  assert.ok(walk(node).some((n) => n.textContent === 'Καμία νέα ιδέα. Η λάμπα τις κρατάει για σένα.'));
  assert.equal(byTag(node, 'BUTTON').length, 0);
});

test('the recording plays on demand and starts the cooldown once', async () => {
  let played = 0;
  const { element, ready } = voiceSection({ lang: 'en', loadVoice: async () => ({ size: 3 }), onPlayed: () => played++, createUrl: () => 'blob:1' });
  const audio = await ready;
  assert.equal(audio.src ?? audio.attrs.src, 'blob:1'); // property in a real DOM, attribute in this double
  assert.ok(audio.controls ?? 'controls' in audio.attrs);
  assert.ok(walk(element).includes(audio));
  audio.listeners.play();
  audio.listeners.play();
  assert.equal(played, 1);
});

test('no recording: said in words, no player', async () => {
  const { element, ready } = voiceSection({ lang: 'en', loadVoice: async () => null });
  assert.equal(await ready, null);
  assert.equal(byTag(element, 'AUDIO').length, 0);
  assert.ok(walk(element).some((n) => n.textContent === 'No recording.'));
});

test('today in one line', () => {
  const node = daySummarySection({ confirmedSteps: 1, relevantMinutes: 35, tasksDone: 2, ideasSaved: 3 }, { lang: 'en' });
  assert.equal(byTag(node, 'P')[0].textContent, '1 step done · 35 min on your goal · 2 tasks done · 3 saved in the lamp');
});
