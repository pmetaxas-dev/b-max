import assert from 'node:assert/strict';
import test from 'node:test';
import { isAuthPage } from '../shared/auth-pages.js';

// KEEP IN SYNC with TestSignInPagesAreNeutral in server/internal/domain/site_test.go.
const CASES = {
  'https://accounts.google.com/o/oauth2/v2/auth?client_id=x': true,
  'https://appleid.apple.com/auth/authorize': true,
  'https://login.microsoftonline.com/common/oauth2/authorize': true,
  'https://www.instagram.com/accounts/login/': true,
  'https://www.instagram.com/accounts/login/two_step_verification?x=1': true,
  'https://www.facebook.com/login.php': true,
  'https://www.facebook.com/dialog/oauth?client_id=1': true,
  'https://www.facebook.com/checkpoint/1501092823525282/': true,
  'https://github.com/login/oauth/authorize?client_id=1': true,
  'https://x.com/i/flow/login': true,
  'https://dev-123.okta.com/app/x/sso/saml': true,
  'https://www.instagram.com/p/abc/': false,
  'https://example.com/loginhelp': false,
  'https://docs.python.org/3/library/getpass.html': false,
  'https://auth0.example.com/docs': false,
  'https://developer.mozilla.org/en-US/docs/Web/HTTP/Authentication': false,
  'https://www.google.com/search?q=login': false,
  'https://notaccounts.google.com.example.org/': false,
  'https://www.reddit.com/r/programming/comments/1/how_i_built_a_login_system/': false,
};

test('sign-in pages are recognised, lookalikes are not', () => {
  for (const [url, auth] of Object.entries(CASES)) assert.equal(isAuthPage(url), auth, url);
});

test('anything unparsable is not a sign-in page', () => {
  assert.equal(isAuthPage('not a url'), false);
  assert.equal(isAuthPage(''), false);
});
