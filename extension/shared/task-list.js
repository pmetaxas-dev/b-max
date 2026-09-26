// The user's tasks (captured with the lamp), most pressing first as the
// server orders them (tasks.go), with a Done button on each open one.
// A done task adds a little to the planet: +0.5%, at most 10% in total.

import { button, el } from './a11y.js';
import { t } from './i18n.js';

// The local day as the server writes it (domain.DayKey), not UTC.
export function localDay(d = new Date()) {
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function taskList(tasks, onDone, { lang, headingId = 'tasks-h', today = localDay() } = {}) {
  const tx = t(lang);
  const item = (task) => {
    const tags = [
      task.overdue ? tx.taskOverdueTag : null,
      !task.overdue && task.urgent ? tx.taskUrgentTag : null,
      task.due && task.due > today ? tx.taskDueTomorrow : null,
    ].filter(Boolean);
    const label = tags.length ? `${task.text} (${tags.join(', ')})` : task.text;
    return task.doneAt
      ? el('li', { class: 'task-done' }, el('s', { text: task.text }), ' ✓')
      : el('li', {}, el('span', { text: `${label} ` }), button(tx.taskDone, () => onDone(task), { class: 'secondary' }));
  };
  return el('section', { 'aria-labelledby': headingId },
    el('h2', { id: headingId, text: tx.tasksTitle }),
    tasks.length ? el('ul', { class: 'tasks' }, ...tasks.map(item)) : el('p', { class: 'muted', text: tx.tasksEmpty }),
  );
}
