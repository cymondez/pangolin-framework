import {mkdirSync, copyFileSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const snippets = path.join(site, 'content/_snippets');
mkdirSync(snippets, {recursive: true});
for (const sample of ['hello', 'greeting-app', 'configuration', 'dependency-injection', 'lifecycle', 'event-envelope']) {
  copyFileSync(path.resolve(site, '../../examples/basics', sample, 'main.go'), path.join(snippets, `${sample}.go`));
}
// Local imports let Docusaurus freeze the code assets together with each manual.
console.log('Synced six current examples; release snapshots retain their own code assets.');
