const fs = require('node:fs');
const path = require('node:path');

function versionState(siteDir = path.resolve(__dirname, '..')) {
  const productVersion = fs.readFileSync(path.resolve(siteDir, '../../VERSION'), 'utf8').trim();
  if (!/^\d+\.\d+\.\d+$/.test(productVersion)) throw new Error('VERSION must be a complete product version');
  const pkg = JSON.parse(fs.readFileSync(path.join(siteDir, 'package.json'), 'utf8'));
  if (pkg.version !== productVersion) throw new Error('docs package version differs from VERSION');
  const lockPath = path.join(siteDir, 'package-lock.json');
  if (fs.existsSync(lockPath)) {
    const lock = JSON.parse(fs.readFileSync(lockPath, 'utf8'));
    if (lock.version !== productVersion || lock.packages[''].version !== productVersion) {
      throw new Error('docs lockfile version differs from VERSION');
    }
  }
  const versionsPath = path.join(siteDir, 'versions.json');
  const released = fs.existsSync(versionsPath) ? JSON.parse(fs.readFileSync(versionsPath, 'utf8')) : [];
  if (!Array.isArray(released) || released.some(v => !/^\d+\.\d+\.\d+$/.test(v)) || new Set(released).size !== released.length) {
    throw new Error('versions.json must list unique released product versions');
  }
  const frozen = released.includes(productVersion);
  return {
    productVersion, released,
    currentLabel: frozen ? 'Next（開發中）' : `v${productVersion}`,
    currentPath: frozen ? 'next' : productVersion,
    lastVersion: frozen ? productVersion : 'current',
  };
}

module.exports = {versionState};
