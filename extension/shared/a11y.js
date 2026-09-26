// DOM helpers for pages and the content script (never imported by the worker).
// Interactive elements are always real <button>/<input>/<a>: `el` refuses to
// attach a click handler to anything else, so a clickable non-button cannot slip in.

const INTERACTIVE = new Set(['button', 'a', 'input', 'select', 'textarea', 'summary', 'form']);

export function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (value === undefined || value === null || value === false) continue;
    if (key.startsWith('on')) {
      if (!INTERACTIVE.has(tag)) {
        throw new Error(`<${tag}> is not interactive; use a real <button> instead of an event handler on it`);
      }
      node.addEventListener(key.slice(2).toLowerCase(), value);
    } else if (key === 'text') {
      node.textContent = value;
    } else if (key in node && key !== 'list' && key !== 'form') {
      node[key] = value;
    } else {
      node.setAttribute(key, value === true ? '' : String(value));
    }
  }
  for (const child of children.flat()) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child);
  }
  return node;
}

export function button(label, onClick, props = {}) {
  return el('button', { type: 'button', text: label, onclick: onClick, ...props });
}

/** Writes to a polite live region so screen readers announce it. */
export function announce(region, text) {
  region.textContent = '';
  // Re-setting on the next frame makes repeated identical messages announce again.
  requestAnimationFrame(() => {
    region.textContent = text;
  });
}

export function clear(node) {
  node.replaceChildren();
}
