#!/usr/bin/env bash
# Phase 2 checks: Go tests, extension unit tests, syntax, and the hard rules
# that can be verified statically. Run from the repository root.
set -u
cd "$(dirname "$0")/.."
fail=0
say() { printf '%s\n' "$*"; }

say "== go vet + test"
# gofmt is checked on the text with Windows line endings removed, so a checkout with CRLF is not reported.
(cd server && bad=$(find . -name '*.go' -not -path './.go-cache/*' | while read -r f; do tr -d '\r' < "$f" | gofmt -l | grep -q . && echo "$f"; done); [ -z "$bad" ] || { say "gofmt needed: $bad"; exit 1; }; go vet ./... && go test -count=1 ./...) || fail=1

say "== extension unit tests"
node --test extension/tests/*.test.mjs || fail=1

say "== extension modules parse, manifest paths and imports resolve"
node scripts/check-extension.mjs || fail=1

say "== no setTimeout/setInterval calls in the service worker or anything it imports"
# Worker graph: background/* plus shared/api.js, shared/messaging.js, shared/state.js.
if grep -nE '\b(setTimeout|setInterval)\s*\(' extension/background/*.js extension/shared/api.js extension/shared/messaging.js extension/shared/state.js; then
  say "FOUND timer call in the worker graph"; fail=1
fi
# The worker must not import DOM-oriented modules either.
if grep -nE "from '\.\./(shared/a11y|shared/planet-placeholder|content|summary|dashboard)" extension/background/*.js; then
  say "worker imports a DOM module"; fail=1
fi

say "== no <div>/<span> used as controls"
if grep -nE "<(div|span)[^>]*(onclick|role=\"button\"|tabindex)" extension --include=*.html --include=*.js -r; then
  say "FOUND non-semantic control"; fail=1
fi
if grep -nE "el\('(div|span|li|p|section)'[^)]*\bon[a-z]+:" -r extension --include=*.js; then
  say "FOUND event handler on a non-interactive element"; fail=1
fi

say "== no bundler artefacts"
ls extension/package.json extension/node_modules extension/vite.config.* 2>/dev/null && { say "bundler files present"; fail=1; }

if [ "$fail" -eq 0 ]; then say "ALL CHECKS PASSED"; else say "CHECKS FAILED"; fi
exit "$fail"
