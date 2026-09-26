// Static checks for the extension: every module parses as an ES module, and
// every path named in manifest.json exists. Run from the repository root.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

const walk = (dir) =>
  fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? walk(path.join(dir, e.name)) : [path.join(dir, e.name)]));

let failures = 0;

for (const file of walk('extension').filter((f) => /\.m?js$/.test(f))) {
  try {
    execFileSync(process.execPath, ['--input-type=module', '--check'], { input: fs.readFileSync(file), stdio: 'pipe' });
    console.log('ok   ', file);
  } catch (err) {
    failures++;
    console.log('FAIL ', file, String(err.stderr).split('\n').slice(0, 4).join(' | '));
  }
}

const manifest = JSON.parse(fs.readFileSync('extension/manifest.json', 'utf8'));
const referenced = [
  manifest.background.service_worker,
  manifest.action.default_popup,
  ...manifest.content_scripts.flatMap((c) => c.js),
  ...manifest.web_accessible_resources.flatMap((r) => r.resources),
  ...(manifest.sandbox?.pages ?? []),
];
const missing = referenced.filter((f) => !fs.existsSync(path.join('extension', f)));
if (missing.length) {
  failures++;
  console.log('manifest references missing files:', missing);
} else {
  console.log('manifest paths: all present');
}

// Every relative import must resolve to a real file.
for (const file of walk('extension').filter((f) => /\.m?js$/.test(f))) {
  const src = fs.readFileSync(file, 'utf8');
  for (const m of src.matchAll(/from\s+'(\.[^']+)'|import\(\s*'(\.[^']+)'\s*\)/g)) {
    const target = path.join(path.dirname(file), m[1] ?? m[2]);
    if (!fs.existsSync(target)) {
      failures++;
      console.log(`FAIL  ${file}: import of ${m[1] ?? m[2]} does not resolve`);
    }
  }
}

process.exit(failures ? 1 : 0);
