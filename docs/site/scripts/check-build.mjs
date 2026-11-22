import assert from 'node:assert/strict';
import {readdirSync, readFileSync, existsSync, statSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
import {versionState} from './version.cjs';

const siteDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const build = path.join(siteDir, 'build');
const state = versionState(siteDir);
const base = process.env.DOCS_BASE_URL || '/';
function files(dir) {
  return readdirSync(dir, {withFileTypes: true}).flatMap(entry => entry.isDirectory() ? files(path.join(dir, entry.name)) : [path.join(dir, entry.name)]);
}
const all = files(build);
const html = all.filter(name => name.endsWith('.html'));
assert(html.length >= 20, 'Expected the complete manual, home, versions, search and 404 pages');
assert(existsSync(path.join(build, 'docs', state.currentPath, 'index.html')), 'Current docs must use their versioned URL');
for (const version of state.released) assert(existsSync(path.join(build, 'docs', version, 'index.html')), `Missing archived docs ${version}`);
assert(existsSync(path.join(build, '404.html')), 'Missing static 404 page');
const manifest = JSON.parse(readFileSync(path.join(build, 'version-manifest.json'), 'utf8'));
assert.deepEqual(manifest, state, 'Build metadata differs from VERSION');
const indexes = all.filter(name => /search-index.*\.json$/.test(name));
assert(indexes.length > 0, 'Missing local search index');
assert(indexes.some(name => readFileSync(name, 'utf8').includes('Required')), 'Search must index manual content');
let links = 0;
for (const file of html) {
  const source = readFileSync(file, 'utf8');
  const route = path.relative(build, file).replaceAll(path.sep, '/').replace(/index\.html$/, '');
  for (const [, raw] of source.matchAll(/\bhref="([^"]+)"/g)) {
    const href = raw.replaceAll('&amp;', '&');
    if (/^(?:https?:|mailto:|tel:|data:|javascript:)/i.test(href)) continue;
    const url = new URL(href, `http://docs.local${base}${route}`);
    if (!url.pathname.startsWith(base)) throw new Error(`Link escapes base path in ${route}: ${href}`);
    const relative = decodeURIComponent(url.pathname.slice(base.length));
    const candidate = path.resolve(build, relative);
    assert(candidate === build || candidate.startsWith(build + path.sep), 'Link escapes build directory');
    const target = existsSync(candidate) && statSync(candidate).isDirectory() ? path.join(candidate, 'index.html') : candidate;
    assert(existsSync(target), `Broken static link in ${route}: ${href}`);
    if (url.hash && target.endsWith('.html')) {
      const id = decodeURIComponent(url.hash.slice(1));
      const targetHtml = readFileSync(target, 'utf8');
      assert(targetHtml.includes(`id="${id}"`), `Broken anchor in ${route}: ${href}`);
    }
    links++;
  }
}
console.log(`Static checks passed: ${html.length} pages, ${links} internal links, ${indexes.length} local search indexes, Framework ${state.productVersion}.`);
