// Dictation on any website. The page asks (MIC_START); the worker keeps the
// hidden recording page alive (offscreen/mic.js) and hands what it hears back to
// the tab. The worker stores the tab in chrome.storage.session, never in
// memory: it may be stopped between two messages. No timers here.

import { MSG } from '../shared/messaging.js';
import { getSession, setSession } from '../shared/state.js';

const TAB_KEY = 'micTab';
const PENDING_KEY = 'micPending';
const OFFSCREEN_URL = 'offscreen/mic.html';
const PERMISSION_URL = 'offscreen/permission.html';

async function hasOffscreen() {
  const contexts = await chrome.runtime.getContexts({ contextTypes: ['OFFSCREEN_DOCUMENT'] });
  return contexts.length > 0;
}

const toOffscreen = (type, extra = {}) =>
  chrome.runtime.sendMessage({ target: 'offscreen', type, ...extra }).catch(() => {});

export async function startMic(tabId, lang) {
  await setSession(TAB_KEY, tabId ?? null);
  if (await hasOffscreen()) {
    await toOffscreen(MSG.MIC_START, { lang });
    return;
  }
  // A new page needs a moment to start listening: it says "ready" and only then is it told to record.
  await setSession(PENDING_KEY, { lang });
  await chrome.offscreen.createDocument({
    url: OFFSCREEN_URL,
    reasons: ['USER_MEDIA'],
    justification: "Record the user's voice so Max can hear them on any website.",
  });
}

export async function stopMic({ abort = false } = {}) {
  await setSession(PENDING_KEY, null);
  if (await hasOffscreen()) await toOffscreen(abort ? MSG.MIC_ABORT : MSG.MIC_STOP);
}

/** What the recording page reports; forwarded to the tab that asked. */
export async function micEvent(event) {
  if (event.event === 'ready') {
    const pending = await getSession(PENDING_KEY, null);
    if (pending) {
      await setSession(PENDING_KEY, null);
      await toOffscreen(MSG.MIC_START, { lang: pending.lang });
    }
    return;
  }
  if (event.event === 'error' && event.code === 'not-allowed') {
    // The extension has no microphone permission yet: ask once, in a small window.
    chrome.windows.create({ url: chrome.runtime.getURL(PERMISSION_URL), type: 'popup', width: 440, height: 300 }).catch(() => {});
  }
  const tabId = await getSession(TAB_KEY, null);
  if (tabId != null) {
    chrome.tabs.sendMessage(tabId, { type: MSG.MIC_EVENT, ...event }, { frameId: 0 }).catch(() => {});
  }
  if (event.event === 'end') chrome.offscreen.closeDocument().catch(() => {});
}
