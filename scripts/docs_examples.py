"""Run the public tutorials as a consumer in a separate Go module."""
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import m0

ROOT = Path(__file__).resolve().parents[1]


def main():
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
    run(['go', 'mod', 'tidy'])
    run(['go', 'build', './...'])
    report = {'status': 'passed', 'framework_version': version, 'consumer_project': str(project),
              'samples': list(samples), 'commands': commands}
    output = ROOT / 'artifacts/docs'
    output.mkdir(parents=True, exist_ok=True)
    (output / 'beginner-examples.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(f'PASS: {len(samples)} runnable consumer examples, environment overrides, invalid config, and manual Go snippets.')
    print(f'Consumer project retained: {project}')


if __name__ == '__main__':
    main()
