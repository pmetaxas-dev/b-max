import assert from 'node:assert/strict';
import test, { beforeEach } from 'node:test';
import { localDay, taskList } from '../shared/task-list.js';

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
const text = (node) => (typeof node === 'string' ? node : node.textContent + (node.children ?? []).map(text).join(''));

test('tasks are listed as the server orders them, with Done on the open ones', () => {
  const done = [];
  const node = taskList([
    { id: 'a', text: 'pay rent', overdue: true, due: '2026-09-17' },
    { id: 'b', text: 'send the email', urgent: true, due: '2026-09-18' },
    { id: 'c', text: 'call the bank', due: '2026-09-19' },
    { id: 'd', text: 'buy milk', due: '2026-09-18', doneAt: '2026-09-18T10:00:00Z' },
  ], (task) => done.push(task.id), { lang: 'en', today: '2026-09-18' });
  const items = walk(node).filter((n) => n.tagName === 'LI');
  assert.deepEqual(items.map((li) => text(li).trim()), [
    'pay rent (overdue) ✓ Done',
    'send the email (urgent) ✓ Done',
    'call the bank (tomorrow) ✓ Done',
    'buy milk ✓',
  ]);
  const buttons = walk(node).filter((n) => n.tagName === 'BUTTON');
  assert.equal(buttons.length, 3, 'no Done button on a done task');
  buttons[1].listeners.click();
  assert.deepEqual(done, ['b']);
});

test('no tasks: a hint where to add one, in the goal language', () => {
  const node = taskList([], () => {}, { lang: 'el' });
  assert.equal(walk(node).find((n) => n.tagName === 'H2').textContent, 'Τα tasks σου');
  assert.ok(walk(node).some((n) => n.textContent === 'Κανένα task. Αποθήκευσε ένα από τη λάμπα.'));
});

test('localDay is the local date, like the server', () => {
  assert.equal(localDay(new Date(2026, 0, 5, 23, 30)), '2026-01-05');
});
