import { el } from './a11y.js';
import { t } from './i18n.js';

// Today's mode and effort score for the popup and the dashboard. The score is
// time on goal pages; it is shown, never turned into planet progress.
export function dayLines(summary) {
  const tx = t(summary?.lang);
  const day = summary?.day;
  const lines = [];
  if (day?.mode === 'bad') lines.push(el('p', { class: 'muted', text: tx.badDay }));
  if (day?.relevantMinutes > 0) lines.push(el('p', { class: 'day-score', text: tx.todayScore(day.relevantMinutes) }));
  return lines;
}
