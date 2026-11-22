import assert from 'node:assert/strict';
import {test} from 'node:test';
import {mkdtempSync, mkdirSync, writeFileSync, rmSync} from 'node:fs';
import path from 'node:path';
import {tmpdir} from 'node:os';
import {versionState} from './version.cjs';

test('current development docs, frozen release and next version use distinct routes', () => {
  const root = mkdtempSync(path.join(tmpdir(), 'pangolin-docs-version-'));
  const site = path.join(root, 'docs/site');
  mkdirSync(site, {recursive: true});
  const write = (name, value) => writeFileSync(path.join(site, name), JSON.stringify(value));
  try {
    writeFileSync(path.join(root, 'VERSION'), '0.1.0\n');
    write('package.json', {version: '0.1.0'});
    write('package-lock.json', {version: '0.1.0', packages: {'': {version: '0.1.0'}}});
    assert.equal(versionState(site).currentPath, '0.1.0');
    assert.equal(versionState(site).lastVersion, 'current');
    write('versions.json', ['0.1.0']);
    assert.equal(versionState(site).currentPath, 'next');
    assert.equal(versionState(site).lastVersion, '0.1.0');
    write('package.json', {version: '0.2.0'});
    assert.throws(() => versionState(site), /package version/);
    writeFileSync(path.join(root, 'VERSION'), '0.2.0\n');
    assert.throws(() => versionState(site), /lockfile version/);
    write('package-lock.json', {version: '0.2.0', packages: {'': {version: '0.2.0'}}});
    assert.equal(versionState(site).currentPath, '0.2.0');
    write('versions.json', ['0.1.0', '0.1.0']);
    assert.throws(() => versionState(site), /unique released/);
  } finally {
    const target = path.resolve(root);
    assert(target.startsWith(path.resolve(tmpdir()) + path.sep));
    rmSync(target, {recursive: true});
  }
});
