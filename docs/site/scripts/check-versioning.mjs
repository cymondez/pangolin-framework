// Exercise Docusaurus itself in an isolated fixture; never archive unreleased docs in the real checkout.
import assert from 'node:assert/strict';
import {cpSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync, readdirSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {versionState} from './version.cjs';
const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repo = path.resolve(site, '../..');
// Nested below the site so normal Node resolution finds the existing dependencies,
// without requiring Windows junction/symlink permissions.
const cache = path.join(site, '.cache');
mkdirSync(cache, {recursive: true});
const fixture = mkdtempSync(path.join(cache, 'docs-versioning-'));
const target = path.join(fixture, 'docs/site');
assert(target.startsWith(cache + path.sep));
const excluded = new Set(['node_modules', 'build', '.docusaurus', '.cache']);
mkdirSync(target, {recursive: true});
for (const entry of readdirSync(site)) {
  if (!excluded.has(entry)) cpSync(path.join(site, entry), path.join(target, entry), {recursive: true});
}
cpSync(path.join(repo, 'VERSION'), path.join(fixture, 'VERSION'));
cpSync(path.join(repo, 'examples/basics'), path.join(fixture, 'examples/basics'), {recursive: true});
const version = readFileSync(path.join(repo, 'VERSION'), 'utf8').trim();
const cli = path.join(site, 'node_modules/@docusaurus/core/bin/docusaurus.mjs');
const env = {...process.env, DOCS_BASE_URL: '/framework-test/', DOCS_URL: 'http://localhost:18089', NO_UPDATE_NOTIFIER: 'true'};
function run(argv) {
  const result = spawnSync(process.execPath, argv, {cwd: target, env, stdio: 'inherit'});
  if (result.error) throw result.error;
  assert.equal(result.status, 0, `Failed command ${argv.join(' ')}`);
}
run([path.join(target, 'scripts/sync-snippets.mjs')]);
if (!versionState(target).released.includes(version)) run([cli, 'docs:version', version]);
const intro = path.join(target, 'content/intro.md');
writeFileSync(intro, readFileSync(intro, 'utf8').replace(/^title:.*$/m, 'title: Next fixture marker'));
const hello = path.join(fixture, 'examples/basics/hello/main.go');
writeFileSync(hello, readFileSync(hello, 'utf8').replace('Hello, panGOlin!', 'Next code marker'));
run([path.join(target, 'scripts/sync-snippets.mjs')]);
run([cli, 'build']);
run([path.join(target, 'scripts/check-build.mjs')]);
const build = path.join(target, 'build');
const releasedIntro = readFileSync(path.join(build, `docs/${version}/index.html`), 'utf8');
const nextIntro = readFileSync(path.join(build, 'docs/next/index.html'), 'utf8');
assert(!releasedIntro.includes('Next fixture marker'), 'Changing current content must not mutate frozen docs');
assert(nextIntro.includes('Next fixture marker'));
const releasedExample = readFileSync(path.join(build, `docs/${version}/getting-started/quickstart/index.html`), 'utf8');
const nextExample = readFileSync(path.join(build, 'docs/next/getting-started/quickstart/index.html'), 'utf8');
assert(!releasedExample.includes('Next code marker'), 'Released examples must not change with current source');
assert(nextExample.includes('Next code marker'), 'Current examples must reflect their own source');
const releasedUow = readFileSync(path.join(build, `docs/${version}/data/unit-of-work/index.html`), 'utf8');
const nextUow = readFileSync(path.join(build, 'docs/next/data/unit-of-work/index.html'), 'utf8');
assert(releasedUow.includes('href="/framework-test/docs/next/data/unit-of-work/"'), 'Switch must retain the corresponding doc');
assert(nextUow.includes(`href="/framework-test/docs/${version}/data/unit-of-work/"`));
assert(releasedUow.includes(`href="/framework-test/docs/${version}/samples/"`));
assert(!releasedUow.includes('href="/framework-test/docs/next/samples/"'), 'Archive navigation must not jump to current samples');
assert(!releasedUow.includes('href="/framework-test/docs/next/getting-started/installation/"'), 'Footer must retain the selected version');
const nextIndex = readdirSync(path.join(build, 'docs/next')).find(name => /^search-index.*json$/.test(name));
const releasedIndex = readdirSync(build).find(name => /^search-index.*json$/.test(name));
assert(nextIndex && releasedIndex, 'Each version must have its own search index');
assert(readFileSync(path.join(build, 'docs/next', nextIndex), 'utf8').includes('Next fixture marker'));
assert(!readFileSync(path.join(build, releasedIndex), 'utf8').includes('Next fixture marker'), 'Archived search must exclude current-only content');
console.log(`Native version snapshot, matching-page switch, isolated content/search, and subpath build passed. Fixture retained at ${fixture}`);
