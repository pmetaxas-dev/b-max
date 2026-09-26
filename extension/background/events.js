// Turns browser signals into POST /browser/event and routes the one command
// that comes back. This file knows what is happening in the browser; it decides
// nothing (architecture-plan-v2 §3). No setTimeout, no setInterval.

import * as api from '../shared/api.js';
import { MSG } from '../shared/messaging.js';
import { setCapturePermission } from '../shared/state.js';
import { call, isAvailable, probe } from './server-client.js';

// Signals are processed one at a time so responses cannot overtake each other.
// (An in-memory chain is fine: if the worker dies, a fresh one starts empty.)
let queue = Promise.resolve();
let contextRevision = 0;
let actionRevision = 0;
let pendingActions = 0;

// Transport barrier only: Go supplies the policy and dismissal scope.
export function beginUnsolicitedAction() {
  pendingActions++;
  actionRevision++;
  return () => { pendingActions--; actionRevision++; };
}

export function signal(type, activity) {
  if (type !== 'heartbeat') contextRevision++;
  queue = queue.then(() => handle(type, activity)).catch(() => {});
  return queue;
}

/**
 * One snapshot of "what Chrome is showing": the active tab of the last focused
 * window, and whether Chrome has focus. Incognito windows are reported as an
 * empty page so nothing from them reaches the server.
 */
async function snapshot() {
  const activity = await chrome.idle.queryState(60);
  let win;
  try {
    win = await chrome.windows.getLastFocused({ windowTypes: ['normal'] });
  } catch {
    return { tabId: -1, url: '', title: '', focused: false, activity };
  }
  const [tab] = await chrome.tabs.query({ active: true, windowId: win.id });
  if (!tab || win.incognito) {
    return { tabId: tab?.id ?? -1, url: '', title: '', focused: !!win.focused, activity };
  }
  return {
    tabId: tab.id, windowId: win.id, url: tab.url ?? '', title: tab.title ?? '',
    focused: !!win.focused, activity, loading: tab.status === 'loading' || !!tab.pendingUrl,
  };
}

async function handle(type, activity) {
  if (!(await isAvailable())) {
    // Only the heartbeat may probe; every other signal is dropped.
    if (type !== 'heartbeat' || !(await probe())) return;
  }
  const revision = contextRevision;
  const action = actionRevision;
  const snap = await snapshot();
  if (activity !== undefined) snap.activity = activity;
  let page;
  try {
    page = await chrome.tabs.sendMessage(snap.tabId, { type: MSG.PAGE_CONTEXT }, { frameId: 0 });
  } catch { /* no receiver: still report the browser event */ }
  const res = await call(() => api.postEvent({ type, ...snap }));
  if (!res.ok) return;
  await route(res.data, snap, revision, page?.url === snap.url ? page.documentToken : undefined, action);
}

// A command goes to the currently active tab. If that tab cannot receive it
// (no content script, chrome:// page), it is dropped, not queued (§14).
async function route(decision, snap, revision, documentToken, action) {
  const tabId = snap.tabId;
  if (tabId < 0 || !documentToken || snap.loading) return;
  async function stillCurrent() {
    const current = await snapshot();
    return revision === contextRevision && !current.loading &&
      current.tabId === snap.tabId && current.windowId === snap.windowId &&
      current.url === snap.url && current.title === snap.title &&
      current.focused === snap.focused && current.activity === snap.activity;
  }
  try {
    if (!(await stillCurrent())) return;
    const allowed = decision.payload?.captureAnchor === true;
    await setCapturePermission(tabId, allowed);
    await chrome.tabs.sendMessage(tabId, { type: MSG.CAPTURE_PERMISSION, allowed, documentToken, url: snap.url }, { frameId: 0 });
    if (decision.command !== 'DO_NOTHING' && snap.focused && snap.activity === 'active' && await stillCurrent() &&
        pendingActions === 0 && action === actionRevision) {
      await chrome.tabs.sendMessage(tabId, {
        type: MSG.COMMAND,
        documentToken,
        url: snap.url,
        title: snap.title,
        command: decision.command,
        reason: decision.reason,
        payload: decision.payload,
      }, { frameId: 0 });
    }
    // After the command, so a card is never delayed by it.
    if (decision.payload?.readMetadata === true) await forwardMetadata(tabId, snap.url, documentToken);
  } catch {
    /* no receiver: dropped */
  }
}

// MVP §4, "metadata as a shield": on a blacklisted page (the server's
// permission) the page's channel/description go to the server, which uses
// them for relevance from the next event on. Any failure changes nothing.
async function forwardMetadata(tabId, url, documentToken) {
  try {
    const meta = await chrome.tabs.sendMessage(tabId, { type: MSG.PAGE_METADATA, documentToken, url }, { frameId: 0 });
    if (meta?.channel || meta?.description) {
      await call(() => api.postMetadata({ url, channel: meta.channel ?? '', description: meta.description ?? '' }));
    }
  } catch {
    /* no receiver */
  }
}

/** A page that starts loading loses any capture permission until Go says otherwise. */
export function revokeCapture(tabId) {
  contextRevision++; // also invalidates responses across same-URL reloads
  return setCapturePermission(tabId, false);
}
