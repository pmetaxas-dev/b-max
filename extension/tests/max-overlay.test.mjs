import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

// Static cascade checks for the Max overlay. Both stylesheets live in the
// same shadow root and max.css is appended AFTER page-object.js's own
// <style>, so a page-object rule must out-rank max.css on specificity, not
// rely on source order.
const pageObject = fs.readFileSync(new URL('../content/page-object.js', import.meta.url), 'utf8');
const maxCss = fs.readFileSync(new URL('../shared/max/max.css', import.meta.url), 'utf8');

const rule = (css, selector) => {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp(`(^|\\n)${escaped}\\s*\\{([^}]*)\\}`).exec(css)?.[2] ?? null;
};

test('max.css positions the anchor absolutely (the rule page-object must beat)', () => {
  assert.match(rule(maxCss, '.max-anchor') ?? '', /position:\s*absolute/);
});

test('the page overlay keeps Max viewport-anchored with higher specificity', () => {
  const overlay = rule(pageObject, ':host .max-anchor');
  assert.ok(overlay, 'expected a ":host .max-anchor" rule in page-object.js');
  assert.match(overlay, /position:\s*fixed/);
  // A bare ".max-anchor" position rule would lose to max.css on source order.
  assert.equal(/(^|\n)\.max-anchor\s*\{[^}]*position/.test(pageObject), false);
});

test('host isolation cannot be overridden by page styles', () => {
  assert.match(rule(pageObject, ':host') ?? '', /all:\s*initial\s*!important/);
});

test('hover feedback is a glow only, never a movement', () => {
  const hover = rule(pageObject, '.max-anchor > .max:hover > svg,\n.max-anchor > .max:focus-visible > svg');
  assert.ok(hover, 'expected a hover/focus rule for the lamp');
  assert.match(hover, /filter:/);
  assert.doesNotMatch(hover, /transform|top|left|margin|translate|scale/);
});
