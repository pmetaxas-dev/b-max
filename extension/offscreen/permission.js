// Asks for the microphone once, for the whole extension. Opened by the worker
// the first time dictation on a website is refused for lack of permission.

const status = document.getElementById('status');

try {
  const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
  stream.getTracks().forEach((track) => track.stop());
  status.textContent = 'Done. You can press the microphone again. · Έτοιμο. Πάτα ξανά το μικρόφωνο.';
  window.close();
} catch {
  status.textContent = 'The microphone is blocked. Allow it for this extension in the browser settings. · Το μικρόφωνο είναι μπλοκαρισμένο. Επίτρεψέ το από τις ρυθμίσεις.';
}
