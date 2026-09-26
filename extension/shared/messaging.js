// Message contract between content scripts / pages and the service worker.

export const MSG = {
  // page -> worker
  CAPTURE_IDEA: 'capture-idea',
  DELIVERY_ACK: 'delivery-ack',
  RAMP_RESPOND: 'ramp-respond',
  STEP_RESPOND: 'step-respond',
  ANCHOR: 'anchor',
  CAN_CAPTURE: 'can-capture',
  OPEN_SUMMARY: 'open-summary',
  OPEN_FULL: 'open-full',
  DAY_RESPOND: 'day-respond',
  CARD_CLOSE: 'card-close',
  WINDOW_KIND: 'window-kind',
  OPEN_SEARCH: 'open-search',
  TASK_DONE: 'task-done',
  TASK_HELP: 'task-help',
  TASK_ACTION: 'task-action',
  SPEAK: 'speak',
  MIC_START: 'mic-start',
  MIC_STOP: 'mic-stop',
  MIC_ABORT: 'mic-abort',
  MIC_EVENT: 'mic-event',
  STATUS_SPOKEN: 'status-spoken',
  CHAT: 'chat',
  LAMP_STATE: 'lamp-state',
  IDEA_OFFER: 'idea-offer',
  // worker -> page
  PAGE_CONTEXT: 'page-context',
  COMMAND: 'command',
  DISMISS_UNSOLICITED: 'dismiss-unsolicited',
  CAPTURE_PERMISSION: 'capture-permission',
  PAGE_METADATA: 'page-metadata',
  FOCUS_BUBBLE: 'focus-bubble',
  OPEN_MAX: 'open-max',
  DICTATE: 'dictate',
};

/**
 * Sends a message to the worker. The worker always answers with
 * { ok: true, data } or { ok: false, unavailable, error }.
 */
export async function send(message) {
  try {
    return await chrome.runtime.sendMessage(message);
  } catch (err) {
    return { ok: false, unavailable: true, error: String(err) };
  }
}
