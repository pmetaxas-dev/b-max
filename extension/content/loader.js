// Content scripts cannot be declared as ES modules in the manifest, and there is
// no bundler. This classic script dynamically imports the real module, which is
// listed under web_accessible_resources.
(async () => {
  try {
    const mod = await import(chrome.runtime.getURL('content/page-object.js'));
    mod.start();
  } catch {
    // Injection can be blocked by the page (CSP) or the extension reloaded.
    // Tracking continues in the worker; the summary is still in the toolbar.
  }
})();
