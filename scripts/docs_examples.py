"""Run the public tutorials as a consumer in a separate Go module."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import m0

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--database',action='store_true',help='execute the PostgreSQL transaction example against the prepared disposable Bookstore database')
    args=parser.parse_args()
    cache = ROOT / '.cache'
    cache.mkdir(exist_ok=True)
    project = Path(tempfile.mkdtemp(prefix='docs-beginner-', dir=cache)).resolve()
    assert project.is_relative_to(cache.resolve())
    env = m0.go_environment(ROOT)
    env.pop('APP_NAME', None)
    commands = []

    def run(args, expected=None, overrides=None, fails=False):
        result = subprocess.run(args, cwd=project, env={**env, **(overrides or {})},
                                text=True, encoding='utf-8', errors='replace', capture_output=True, timeout=90)
        output = result.stdout.strip()
        if (result.returncode != 0) != fails:
            raise RuntimeError(f'{args}: {result.stdout}{result.stderr}')
        if expected is not None and output != expected:
            raise ValueError(f'{args}: expected {expected!r}, got {output!r}')
        commands.append({'argv': args, 'exit_code': result.returncode, 'stdout': output})
        return output

    version = (ROOT / 'VERSION').read_text().strip()
    run(['go', 'mod', 'init', 'example.com/hello-pangolin'])
    run(['go', 'mod', 'edit', '-require=github.com/cymondez/pangolin-framework@v' + version])
    run(['go', 'mod', 'edit', '-replace=github.com/cymondez/pangolin-framework=' + ROOT.as_posix()])
    samples = {
        'hello': 'Hello, panGOlin!',
        'greeting-app': 'Hello, World!',
        'configuration': 'Application: My App',
        'dependency-injection': 'Hello, Alice!',
        'lifecycle': 'start storage\nstart books\nready: true\nstop books\nstop storage',
        'event-envelope': 'event-1 / bookstore.book_created / schema 1\n{"id":"book-1","title":"Go in Action"}',
        'observability': 'Trace correlated: true\n/livez: 204\n/readyz: 204\nJSON log recorded: true',
        'authorization': 'read allowed\nwrite denied\nfeature enabled: write denied',
        'platform-services': 'Tenant cache isolated: true\nTenant setting: "blue"\nJSON array setting: "mail"\nSetting layers: [user tenant global config default]\nEffective quota: 10\nFeature cannot grant permission: true\nStored update despite notification failure: true\nVersion converged: true',
    }
    for sample, expected in samples.items():
        shutil.copyfile(ROOT / f'examples/basics/{sample}/main.go', project / 'main.go')
        run(['go', 'mod', 'tidy'])
        run(['go', 'run', '.'], expected)
        if sample == 'greeting-app':
            run(['go', 'run', '.'], 'Hello, Reader!', {'APP_NAME': 'Reader'})
        if sample == 'configuration':
            run(['go', 'run', '.'], 'Application: Bookstore', {'APP_NAME': 'Bookstore'})
            run(['go', 'run', '.'], overrides={'APP_NAME': ''}, fails=True)
    # Compile the complete, app-owned repository/service examples printed in the manual.
    books = project / 'books'
    books.mkdir()
    for document, target in [('data/unit-of-work.md', 'service.go'), ('data/postgresql.md', 'repository.go')]:
        source = (ROOT / 'docs/site/content' / document).read_text(encoding='utf-8')
        code = re.search(r'```go\n(.*?)```', source, re.S).group(1)
        (books / target).write_text(code, encoding='utf-8')
    setup=project/'setup';setup.mkdir()
    guide=(ROOT/'docs/site/content/security/resource-scopes.md').read_text(encoding='utf-8')
    (setup/'organization.go').write_text(re.search(r'```go\n(.*?)```',guide,re.S).group(1),encoding='utf-8')
    for document, folder, name in [
        ('data/idempotency.md', 'orders', 'service.go'),
        ('data/migrations.md', 'setup', 'deployment.go'),
        ('events/reliable-delivery.md', 'setup', 'events.go'),
        ('platform/jobs.md', 'setup', 'jobs.go'),
    ]:
        guide=(ROOT/'docs/site/content'/document).read_text(encoding='utf-8')
        destination=project/folder;destination.mkdir(exist_ok=True)
        (destination/name).write_text(re.search(r'```go\n(.*?)```',guide,re.S).group(1),encoding='utf-8')
    shutil.copyfile(ROOT/'examples/basics/transactions/main.go',project/'main.go')
    run(['go', 'mod', 'tidy'])
    run(['go', 'build', './...'])
    if args.database:
        if not os.getenv('PANGOLIN_DSN'):raise ValueError('--database requires PANGOLIN_DSN for the disposable Bookstore database')
        run(['go','run','.'],'Required shares root: true\nRequiresNew has new root: true\nParent rolled back: true\nChild committed: true\nScope closed: true')
    report = {'status': 'passed', 'framework_version': version, 'consumer_project': str(project),
              'samples': list(samples)+(['transactions'] if args.database else []), 'database_example_executed':args.database, 'commands': commands}
    output = ROOT / 'artifacts/docs'
    output.mkdir(parents=True, exist_ok=True)
    (output / 'beginner-examples.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(f'PASS: {len(report["samples"])} runnable consumer examples, environment overrides, invalid config, and manual Go snippets.')
    print(f'Consumer project retained: {project}')


if __name__ == '__main__':
    main()
