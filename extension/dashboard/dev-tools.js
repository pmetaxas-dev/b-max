// The dashboard's ⚙️ test panel (hackathon build only): fresh day, progress
// reset, profile reset and fast timings, so trying the app by hand never
// needs state.json edits. The server does the work (dev.go); this confirms
// with the user first and clears what the extension itself remembers.

export const PROFILE_CONFIRM_WORD = 'DELETE';

// Only the planet's last-known era is extension-side state tied to the profile;
// Max's position is the user's, not the profile's, and stays.
const PROFILE_STORAGE_KEYS = ['planetState'];

/**
 * Asks, then has the planner rewrite the current milestone's undone steps
 * (done steps stay). Returns true if it happened.
 */
export async function confirmAndRegenerate({ api, confirm, text }) {
  if (!confirm(text.devRegenerateConfirm)) return false;
  await api.devRegenerateSteps();
  return true;
}

/**
 * Asks for confirmation, then resets. Returns true if the reset happened.
 * deps: { api, confirm(text) -> bool, prompt(text) -> string|null, storage, text }
 */
export async function confirmAndReset(scope, { api, confirm, prompt, storage, text }) {
  if (scope === 'profile') {
    const typed = prompt(text.devProfileConfirm(PROFILE_CONFIRM_WORD));
    if ((typed ?? '').trim().toUpperCase() !== PROFILE_CONFIRM_WORD) return false;
  } else if (!confirm(scope === 'day' ? text.devDayConfirm : text.devProgressConfirm)) {
    return false;
  }
  await api.devReset(scope);
  if (scope === 'profile') {
    try {
      await storage?.remove(PROFILE_STORAGE_KEYS);
    } catch {
      /* only the offline planet fallback is affected */
    }
  }
  return true;
}
