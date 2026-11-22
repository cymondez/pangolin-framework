import {spawnSync} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {versionState} from './version.cjs';

const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const {productVersion} = versionState(site);
const project = process.env.PANGOLIN_DOCS_PROJECT || 'pangolin-docs';
if (!/^[a-z0-9][a-z0-9_-]*$/.test(project)) throw new Error('Invalid Compose project name');
const port = process.env.PANGOLIN_DOCS_PORT || '18088';
if (!/^\d+$/.test(port) || Number(port) < 1 || Number(port) > 65535) throw new Error('Invalid docs port');
const args = process.argv.slice(2);
if (!['up', 'down', 'build', 'config', 'ps', 'logs', 'stop'].includes(args[0])) {
  throw new Error('Usage: node docs/site/scripts/docker.mjs <up|down|build|config|ps|logs|stop> [Compose options]');
}
// No shell interpolation; VERSION is the authoritative image/build version.
const result = spawnSync('docker', ['compose', '-p', project, '-f', path.join(site, 'compose.yaml'), ...args], {
  stdio: 'inherit', env: {...process.env, PANGOLIN_VERSION: productVersion, DOCS_URL: process.env.DOCS_URL || `http://localhost:${port}`},
});
if (result.error) throw result.error;
process.exit(result.status ?? 1);
