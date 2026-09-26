import assert from 'node:assert/strict';
import test, { beforeEach } from 'node:test';
import { donePrompt, DONE_PROMPT_TITLE } from '../shared/done-prompt.js';

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

const walk = (node) => [node, ...(node.children ?? []).flatMap(walk)];

// Manual test: after "Not yet" on the page, the popup asked the same question
// again, and its "Not yet" did nothing. It is now a standing offer: one Yes.
test('the popup offers only "Yes, I\'m done", never a second "Not yet"', () => {
  let done = 0;
  const node = donePrompt({ id: 's1', text: 'Design channel branding' }, () => done++);
  const all = walk(node);
  assert.equal(all.find((n) => n.tagName === 'H2').textContent, DONE_PROMPT_TITLE);
  const buttons = all.filter((n) => n.tagName === 'BUTTON');
  assert.deepEqual(buttons.map((b) => b.textContent), ["Yes, I'm done"]);
  buttons[0].listeners.click();
  assert.equal(done, 1);
  assert.ok(all.some((n) => n.textContent === 'Design channel branding'));
});
