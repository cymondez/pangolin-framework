import {mkdirSync, copyFileSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repo = path.resolve(site, '../..');
const snippets = path.join(site, 'content/_snippets');
mkdirSync(snippets, {recursive: true});
for (const sample of ['hello', 'greeting-app', 'root-greeting', 'configuration', 'dependency-injection', 'root-lifecycle', 'lifecycle', 'event-envelope', 'transactions', 'authorization', 'observability', 'platform-services']) {
  copyFileSync(path.resolve(site, '../../examples/basics', sample, 'main.go'), path.join(snippets, `${sample}.go`));
}
// The cross-module contract manual quotes the real imported contracts package
// and modules of the formal moduleapp example, so it stays runnable verbatim.
const moduleapp = [
  ['examples/moduleapp/catalog/contracts/contracts.go', 'moduleapp-contracts.go'],
  ['examples/moduleapp/catalog/module.go', 'moduleapp-catalog.go'],
  ['examples/moduleapp/app/module.go', 'moduleapp-app.go'],
];
for (const [source, target] of moduleapp) {
  copyFileSync(path.resolve(repo, source), path.join(snippets, target));
}
console.log('Synced twelve basics examples and the moduleapp contract example; release snapshots retain their own code assets.');
