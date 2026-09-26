// Sign-in / authentication pages: no Max there (the server also treats them
// as neutral: no card, no time). KEEP IN SYNC with IsAuthPage in
// server/internal/domain/site.go; both have the same table test.

const AUTH_HOSTS = [
  'accounts.google.com', 'appleid.apple.com', 'idmsa.apple.com',
  'login.microsoftonline.com', 'login.live.com', 'login.yahoo.com',
  'auth0.com', 'okta.com',
];

// Whole path segments only ("/accounts/login/", "/login.php"), so that
// "/loginhelp" or "/docs/authentication" stay normal pages.
const AUTH_SEGMENTS = new Set([
  'oauth', 'oauth2', 'authorize', 'login', 'logon',
  'signin', 'sign-in', 'two_step_verification', '2fa', 'checkpoint',
]);

export function isAuthPage(href) {
  let url;
  try {
    url = new URL(href);
  } catch {
    return false;
  }
  const host = url.hostname.toLowerCase();
  if (AUTH_HOSTS.some((h) => host === h || host.endsWith(`.${h}`))) return true;
  return url.pathname.toLowerCase().split('/').some((segment) => {
    const dot = segment.indexOf('.');
    return AUTH_SEGMENTS.has(dot > 0 ? segment.slice(0, dot) : segment);
  });
}
