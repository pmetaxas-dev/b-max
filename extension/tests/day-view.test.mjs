import assert from 'node:assert/strict';
import test, { beforeEach } from 'node:test';
import { dayLines } from '../shared/day-view.js';
import { t } from '../shared/i18n.js';

beforeEach(() => {
  globalThis.document = { createElement: (tag) => ({ tag, textContent: '', attrs: {}, setAttribute(k, v) { this.attrs[k] = v; }, append() {} }) };
});

const texts = (summary) => dayLines(summary).map((n) => n.textContent);

test('the day score and rest-day note follow the goal language', () => {
  assert.deepEqual(texts({ lang: 'el', day: { mode: 'good', relevantMinutes: 25 } }), ['Σήμερα: 25′ σε σελίδες του στόχου σου 👍']);
  assert.deepEqual(texts({ lang: 'en', day: { mode: 'bad', relevantMinutes: 3 } }), ['Rest day — no interruptions today.', 'Today: 3 min on goal pages 👍']);
});

test('nothing is shown for an undecided day without goal time, or an old server', () => {
  assert.deepEqual(texts({ lang: 'en', day: { mode: 'undecided', relevantMinutes: 0 } }), []);
  assert.deepEqual(texts({}), []);
});

test('both languages define every text', () => {
  assert.deepEqual(Object.keys(t('el')).sort(), Object.keys(t('en')).sort());
  assert.deepEqual(Object.keys(t('el').eras), ['prehistoric', 'copper', 'medieval', 'industrial', 'space']);
  assert.equal(t('fr'), t('en'));
});
