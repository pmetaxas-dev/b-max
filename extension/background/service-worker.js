// MV3 service worker. Coordinates browser events; decides nothing.
//
// HARD RULE (architecture-plan-v2 §5.3): no setTimeout, no setInterval here or
// in any module it imports. The worker is killed after ~30s idle and timers die
// with it. Time lives in Go as timestamps; the 1-minute chrome.alarms heartbeat
// covers the passive case.

import { MSG } from '../shared/messaging.js';
import { handleMessage, openFull } from './actions.js';
import { revokeCapture, signal } from './events.js';
import { speakStatus } from './voice.js';

const HEARTBEAT = 'heartbeat';

async function ensureHeartbeat() {
  if (!(await chrome.alarms.get(HEARTBEAT))) {
    chrome.alarms.create(HEARTBEAT, { periodInMinutes: 0.5 });
  }
}

// Listeners must be registered synchronously at top level so a freshly woken
// worker receives the event that woke it.
chrome.runtime.onInstalled.addListener(({ reason }) => {
  ensureHeartbeat();
  if (reason === 'install') openFull(); // onboarding lives in the full tab
});
chrome.runtime.onStartup.addListener(ensureHeartbeat);
ensureHeartbeat();
chrome.idle.setDetectionInterval(60);
chrome.idle.onStateChanged.addListener((activity) => signal('activity_changed', activity));

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === HEARTBEAT) signal('heartbeat');
});

chrome.tabs.onActivated.addListener(() => signal('tab_activated'));
chrome.tabs.onRemoved.addListener(() => signal('tab_removed'));
chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.status === 'loading') revokeCapture(tabId);
  if (tab.active && (changeInfo.status === 'complete' || changeInfo.url)) signal('tab_updated');
});
chrome.windows.onFocusChanged.addListener((windowId) => {
  signal(windowId === chrome.windows.WINDOW_ID_NONE ? 'window_blur' : 'window_focus');
});

// The planet's own ↗ expand control calls window.open(planet.html?…&view=full).
// The planet is a sandboxed page, so that navigation's initiator is an opaque
// origin, not this extension, and planet.html is not web-accessible: Chrome
// blocks it with ERR_BLOCKED_BY_CLIENT. Re-issuing the exact same URL from the
// extension is always allowed. Once per tab, so a real failure cannot loop.
const PLANET_URL = chrome.runtime.getURL('shared/planet/planet.html');
const reopenedPlanetTabs = new Set();
chrome.webNavigation.onErrorOccurred.addListener(({ tabId, frameId, url, error }) => {
  if (frameId !== 0 || error !== 'net::ERR_BLOCKED_BY_CLIENT' || !url.startsWith(`${PLANET_URL}?`)) return;
  if (reopenedPlanetTabs.has(tabId)) return;
  reopenedPlanetTabs.add(tabId);
  chrome.tabs.update(tabId, { url });
}, { url: [{ urlPrefix: PLANET_URL }] });

// The bubble never steals focus, so it is reachable by shortcut (§12).
chrome.commands.onCommand.addListener(async (command) => {
  if (command === 'read-status') {
    // Anywhere, with no page needed: Max says where the user is.
    speakStatus();
    return;
  }
  if (!['focus-companion', 'open-max', 'dictate'].includes(command)) return;
  const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  const type = { 'open-max': MSG.OPEN_MAX, dictate: MSG.DICTATE }[command] ?? MSG.FOCUS_BUBBLE;
  if (tab?.id === undefined) return;
  try {
    await chrome.tabs.sendMessage(tab.id, { type });
  } catch {
    // A page with no Max (chrome://, the store): the full app instead.
    if (command === 'open-max' || command === 'dictate') openFull();
  }
});

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  handleMessage(message, sender).then(sendResponse, (err) =>
    sendResponse({ ok: false, unavailable: false, error: String(err) }),
  );
  return true; // async response
});
