// The service worker is killed after ~30s of inactivity, so nothing it needs to
// remember may live in module variables. chrome.storage.session survives worker
// restarts (and is cleared when the browser closes).

export async function getSession(key, fallback) {
  const out = await chrome.storage.session.get(key);
  return key in out ? out[key] : fallback;
}

export async function setSession(key, value) {
  await chrome.storage.session.set({ [key]: value });
}

const CAPTURE_KEY = 'capturePermission';

export async function setCapturePermission(tabId, allowed) {
  const map = await getSession(CAPTURE_KEY, {});
  if (allowed) map[tabId] = true;
  else delete map[tabId];
  await setSession(CAPTURE_KEY, map);
}

export async function getCapturePermission(tabId) {
  const map = await getSession(CAPTURE_KEY, {});
  return map[tabId] === true;
}
