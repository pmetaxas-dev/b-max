// Handlers for messages from content scripts and pages, and browser-level
// actions (switch tab, open windows). No timers.

import * as api from '../shared/api.js';
import { MSG } from '../shared/messaging.js';
import { searchUrl } from '../shared/search.js';
import { getCapturePermission } from '../shared/state.js';
import { call } from './server-client.js';
import { beginUnsolicitedAction } from './events.js';
import { speakIfEnabled, speakStatus } from './voice.js';
import { micEvent, startMic, stopMic } from './mic.js';

// section: a part of the full tab to open at ("treasure", "voice"); only
// these known names, never text from a page.
const SECTIONS = new Set(['treasure', 'voice']);
export function openFull(section) {
  const hash = SECTIONS.has(section) ? `#${section}` : '';
  return chrome.tabs.create({ url: chrome.runtime.getURL(`dashboard/dashboard.html${hash}`) });
}

export async function openSummary() {
  try {
    await chrome.action.openPopup();
  } catch {
    // openPopup can be refused (no active window, older Chrome); fall back to a small window.
    await chrome.windows.create({
      url: chrome.runtime.getURL('summary/summary.html'),
      type: 'popup',
      width: 380,
      height: 560,
    });
  }
}

/**
 * Continue: somewhere about the current step, in this order (the server only
 * sends targets from today that are about the goal):
 * 1. the tab of today's latest goal page, if it still shows that page;
 * 2. that page, reopened; 3. today's return anchor;
 * 4. a search for the current step (a phrase from the server, never a link);
 * 5. the full tab. Never merely the last page that happened to be open.
 */
async function performContinue({ lastWorkTabId, lastWorkUrl, anchorUrl, searchQuery }) {
  const workUrl = /^https?:\/\//.test(lastWorkUrl || '') ? lastWorkUrl : '';
  if (lastWorkTabId > 0) {
    try {
      const tab = await chrome.tabs.get(lastWorkTabId);
      if (!tab.incognito && tab.status !== 'loading' && !tab.pendingUrl && workUrl && tab.url === workUrl) {
        await chrome.tabs.update(lastWorkTabId, { active: true });
        await chrome.windows.update(tab.windowId, { focused: true });
        return;
      }
    } catch {
      /* tab closed */
    }
  }
  if (workUrl) {
    await chrome.tabs.create({ url: workUrl });
    return;
  }
  if (anchorUrl) {
    await chrome.tabs.create({ url: anchorUrl });
    return;
  }
  const search = searchUrl(searchQuery);
  if (search) {
    await chrome.tabs.create({ url: search });
    return;
  }
  await openFull();
}

// Search links are built in shared/search.js (also used by extension pages).
export { searchUrl };

async function broadcastDismiss(scope) {
  const tabs = await chrome.tabs.query({});
  await Promise.allSettled(tabs.filter(tab => !tab.incognito).map(tab =>
    chrome.tabs.sendMessage(tab.id, { type: MSG.DISMISS_UNSOLICITED, ...scope }, { frameId: 0 })));
}

export async function handleMessage(message, sender) {
  switch (message?.type) {
    case MSG.CAPTURE_IDEA:
      return call(() => api.captureIdea(message.text, message.itemType));
    case MSG.DELIVERY_ACK: {
      // Reject acknowledgements from a document that is no longer foregrounded.
      const tabId = sender.tab?.id;
      if (tabId === undefined || sender.frameId !== 0 || !sender.documentId) return { ok: false, error: 'stale_delivery' };
      const tab = await chrome.tabs.get(tabId);
      const win = await chrome.windows.get(tab.windowId);
      if (!tab.active || !win.focused || tab.incognito || tab.status === 'loading' || tab.pendingUrl || tab.url !== message.url) {
        return { ok: false, error: 'stale_delivery' };
      }
      const page = await chrome.tabs.sendMessage(tabId, { type: MSG.PAGE_CONTEXT }, { documentId: sender.documentId });
      if (page?.documentToken !== message.documentToken || page.url !== message.url || !page.visible) {
        return { ok: false, error: 'stale_delivery' };
      }
      return call(() => api.acknowledgeDelivery({ deliveryId: message.deliveryId, tabId, url: message.url }));
    }
    case MSG.RAMP_RESPOND: {
      const finish = beginUnsolicitedAction();
      try {
        const res = await call(() => api.rampRespond(message.site, message.choice));
        if (res.ok && res.data.dismissUnsolicited) await broadcastDismiss(res.data.dismissUnsolicited);
        if (res.ok && message.choice === 'continue') await performContinue(res.data);
        return res;
      } finally {
        finish();
      }
    }
    case MSG.STEP_RESPOND:
      return call(() => api.stepsRespond(message.stepId, message.answer));
    case MSG.ANCHOR: {
      // Only accept anchors from a tab Go currently allows us to read.
      const tabId = sender.tab?.id;
      if (tabId === undefined || !(await getCapturePermission(tabId))) {
        return { ok: true, data: { stored: false, reason: 'capture_not_allowed' } };
      }
      return call(() => api.postAnchor(message.anchor));
    }
    case MSG.CAN_CAPTURE:
      return { ok: true, data: sender.tab?.id !== undefined && (await getCapturePermission(sender.tab.id)) };
    case MSG.OPEN_SUMMARY:
      await openSummary();
      return { ok: true };
    case MSG.OPEN_FULL:
      await openFull(message.section);
      return { ok: true };
    case MSG.DAY_RESPOND: {
      const finish = beginUnsolicitedAction();
      try {
        const res = await call(() => api.dayRespond(message.choice));
        if (res.ok && res.data.dismissUnsolicited) await broadcastDismiss(res.data.dismissUnsolicited);
        return res;
      } finally {
        finish();
      }
    }
    case MSG.WINDOW_KIND: {
      // "popup" for sign-in popups; the page itself cannot see its window type.
      if (sender.tab?.windowId === undefined) return { ok: true, data: 'normal' };
      try {
        return { ok: true, data: (await chrome.windows.get(sender.tab.windowId)).type ?? 'normal' };
      } catch {
        return { ok: true, data: 'normal' };
      }
    }
    case MSG.CARD_CLOSE:
      return call(() => api.cardClose(message.deliveryId, message.reason));
    case MSG.TASK_ACTION: {
      const res = await call(() => api.taskAction(message.id, message.action, message.note));
      // "Take me back": the page the user was on, or where the task starts (a search).
      if (res.ok && message.action === 'resume') {
        const url = /^https?:\/\//.test(res.data.resumeUrl || '') ? res.data.resumeUrl : searchUrl(res.data.searchQuery);
        if (url) await chrome.tabs.create({ url });
      }
      return res;
    }
    case MSG.MIC_START:
      await startMic(sender.tab?.id, message.lang);
      return { ok: true };
    case MSG.MIC_STOP:
      await stopMic();
      return { ok: true };
    case MSG.MIC_ABORT:
      await stopMic({ abort: true });
      return { ok: true };
    case MSG.MIC_EVENT:
      await micEvent(message);
      return { ok: true };
    case MSG.SPEAK:
      await speakIfEnabled(message.text, message.lang);
      return { ok: true };
    case MSG.STATUS_SPOKEN:
      return speakStatus();
    case MSG.TASK_HELP:
      return call(() => api.taskHelp(message.id));
    case MSG.CHAT:
      return call(() => api.chat({ message: message.text, channel: 'page', lang: message.lang }));
    case MSG.TASK_DONE:
      return call(() => api.taskDone(message.id));
    case MSG.LAMP_STATE:
      return call(() => api.getLamp());
    case MSG.IDEA_OFFER:
      return call(() => api.answerOldIdea(message.id, !!message.accept));
    case MSG.OPEN_SEARCH: {
      const url = searchUrl(message.query);
      if (!url) return { ok: false, error: 'invalid_query' };
      await chrome.tabs.create({ url });
      return { ok: true };
    }
    default:
      return { ok: false, error: 'unknown_message' };
  }
}
