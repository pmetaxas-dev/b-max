// A short confetti burst for the "reached Space" celebration. Self-contained
// (inline styles + the Web Animations API, no external CSS/keyframes), so it
// is safe to append straight into a page's light DOM from a content script
// as well as from an ordinary extension page. Skipped entirely under reduced
// motion, like every other animation in this codebase.

const PIECE_COLORS = ['#ffd54a', '#60a5fa', '#f472b6', '#34d399', '#fb923c'];
const PIECE_COUNT = 28;
const BASE_DURATION_MS = 2200;

/** Drops a handful of colored pieces from the top of the viewport, fading
 * out as they fall. `root` is where the burst is appended (defaults to
 * document.body); pass a Shadow DOM host's light-DOM ancestor if needed. */
export function burstConfetti(root = globalThis.document?.body) {
  if (!root || typeof root.appendChild !== 'function') return;
  if (globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;

  const layer = globalThis.document.createElement('div');
  layer.setAttribute('aria-hidden', 'true');
  Object.assign(layer.style, {
    position: 'fixed', inset: '0', pointerEvents: 'none', overflow: 'hidden', zIndex: '2147483647',
  });

  let longestMs = BASE_DURATION_MS;
  for (let i = 0; i < PIECE_COUNT; i++) {
    const piece = globalThis.document.createElement('span');
    Object.assign(piece.style, {
      position: 'absolute', top: '-16px', left: `${Math.random() * 100}%`,
      width: '8px', height: '14px', background: PIECE_COLORS[i % PIECE_COLORS.length],
      opacity: '0.9', borderRadius: '2px',
    });
    layer.append(piece);
    const duration = BASE_DURATION_MS - 400 + Math.random() * 800;
    const delay = Math.random() * 300;
    longestMs = Math.max(longestMs, duration + delay);
    if (typeof piece.animate !== 'function') continue; // no WAAPI: stays put, still visible
    piece.animate(
      [
        { transform: 'translate(0, 0) rotate(0deg)', opacity: 1 },
        { transform: `translate(${(Math.random() - 0.5) * 140}px, 100vh) rotate(${180 + Math.random() * 540}deg)`, opacity: 0 },
      ],
      { duration, delay, easing: 'cubic-bezier(.25,.6,.3,1)', fill: 'forwards' },
    );
  }
  root.appendChild(layer);
  globalThis.setTimeout(() => layer.remove(), longestMs + 300);
}
