#!/usr/bin/env python3
"""Run the archived T0 binary through owned tui-test sessions."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import runpy
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
ARCHIVE = ROOT / 'plans/261007-1156-inline-resize-fix/artifacts/replay-candidate/source'
BASELINE = ROOT / 'plans/261006-1649-t0-inline-prototype-gate/artifacts/phase-05-source'
MARKER = re.compile(r'G1\[\d{4}\]')


def assert_transcript(text, count=2000):
    actual = MARKER.findall(text)
    expected = [f'G1[{i:04d}]' for i in range(count)]
    assert actual == expected, f'ordered transcript differs: retained={len(actual)}, expected={count}'


def assert_cursor(state, x, row):
    lines = state['text'].split('\n')
    assert lines.count(row) == 1, f'expected one row {row!r}: {lines!r}'
    expected = {'x': x, 'y': lines.index(row)}
    assert state['cursor'] == expected, f'cursor={state["cursor"]}, expected={expected}'


def assert_editor(state, first):
    assert state['text'].split('\n').count(first) == 1, f'duplicate or missing editor row: {state}'
    assert state['text'].count('editor>') == 1, f'stale editor rows: {state}'


def output_bytes(recording):
    lines = recording.splitlines()
    header = json.loads(lines[0])
    assert header['version'] == 2, 'recording is not asciicast v2'
    return ''.join(event[2] for event in map(json.loads, lines[1:]) if event[1] == 'o')


def assert_no_replay(raw, count=2000):
    assert_transcript(raw, count)
    for sequence in ('\x1b[2J', '\x1b[3J', '\x1b[?47h', '\x1b[?1047h', '\x1b[?1049h'):
        assert sequence not in raw, f'global erase or alternate screen: {sequence!r}'


def assert_resize_replay(raw, count=2000):
    generations = raw.split('\x1b[2J\x1b[3J\x1b[H')
    assert len(generations) > 1, 'resize repair did not purge and reconstruct history'
    for generation in generations:
        assert_no_replay(generation, count)
        assert generation.count('T0 startup') == 1, 'missing or duplicate startup header in generation'


def wait_replay(session, previous, count=2000):
    def repaired(_):
        raw = session.recording()
        if raw.count('\x1b[2J\x1b[3J\x1b[H') <= previous:
            return False
        try:
            assert_resize_replay(raw, count)
            return True
        except AssertionError:
            return False
    session.wait(repaired)


class Session:
    def __init__(self, cli, name, backend, target, directory, artifacts, scenario):
        self.cli, self.name, self.backend = cli, name, backend
        self.artifacts = artifacts
        artifacts.mkdir(parents=True)
        self.calls = []
        self.started = False
        self.identities = {}
        self.owner = {}
        self.target = str(target)
        self.report = artifacts / 'draft.json'
        status = self.call('daemon', 'status', allow=(3,))
        assert status.returncode == 3, 'refusing to reuse an existing daemon'
        try:
            result = self.call('--verbose', 'run', '--backend', backend, '--cols', '40', '--rows', '12',
                               '--config', str(HERE / 'tui-test.toml'),
                               '--cwd', str(directory), '--env', 'TERM=xterm-256color', str(target),
                               '--scenario', scenario, '--draft-report', str(self.report))
            self.started = True
            self.owner = json.loads(result.stdout)['data']
            self.identities = {pid: self.identity(pid) for pid in (self.owner['pid'], self.owner['shell_pid'])}
            (artifacts / 'ownership.json').write_text(json.dumps(self.owner, indent=2) + '\n')
            self.wait(lambda s: 'editor>' in s['text'])
        except BaseException as original:
            self.discover_owner()
            if self.started:
                self.capture('startup-failure')
            try:
                self.close()
            except Exception as cleanup:
                (self.artifacts / 'startup-cleanup.error').write_text(str(cleanup))
            raise original

    def discover_owner(self):
        if self.owner:
            return
        pid_file = Path.home() / '.tui-test' / f'{self.name}.pid'
        try:
            pid = int(pid_file.read_text().strip())
        except (OSError, ValueError):
            return
        identity = self.identity(pid)
        if not identity or self.name not in identity or 'tui-test' not in identity:
            return
        self.started = True
        self.owner = {'pid': pid}
        self.identities[pid] = identity
        children = subprocess.run(['ps', '-axo', 'pid=,ppid=,command='], capture_output=True, text=True, timeout=2)
        for line in children.stdout.splitlines():
            fields = line.strip().split(None, 2)
            if len(fields) == 3 and fields[1] == str(pid) and self.target in fields[2]:
                child = int(fields[0])
                self.owner['shell_pid'] = child
                self.identities[child] = self.identity(child)
        (self.artifacts / 'ownership.json').write_text(json.dumps(self.owner, indent=2) + '\n')

    def call(self, *args, allow=(), timeout=8):
        command = [self.cli, '--session', self.name, '--json', *args]
        try:
            result = subprocess.run(command, capture_output=True, text=True, timeout=timeout)
            entry = {'command': command, 'code': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr}
        except subprocess.TimeoutExpired as error:
            self.calls.append({'command': command, 'timeout': timeout})
            (self.artifacts / 'commands.json').write_text(json.dumps(self.calls, ensure_ascii=False, indent=2) + '\n')
            raise RuntimeError(f'CLI timed out after {timeout}s: {args}') from error
        self.calls.append(entry)
        (self.artifacts / 'commands.json').write_text(json.dumps(self.calls, ensure_ascii=False, indent=2) + '\n')
        if result.returncode not in (0, *allow):
            raise RuntimeError(f'{args}: exit {result.returncode}: {result.stderr}{result.stdout}')
        return result

    @staticmethod
    def identity(pid):
        result = subprocess.run(['ps', '-p', str(pid), '-o', 'lstart=,command='], capture_output=True, text=True, timeout=2)
        return result.stdout.strip() or None

    def terminate_owned(self, pid):
        original = self.identities.get(pid)
        if not original:
            return
        for sig in (signal.SIGTERM, signal.SIGKILL):
            current = self.identity(pid)
            if current is None:
                return
            if current != original:
                raise RuntimeError(f'PID {pid} identity changed; refusing to signal it')
            os.kill(pid, sig)
            deadline = time.monotonic() + 2
            while time.monotonic() < deadline:
                if self.identity(pid) is None:
                    return
                time.sleep(0.025)
        raise RuntimeError(f'owned PID {pid} did not exit')

    def state(self):
        return json.loads(self.call('state').stdout)['data']

    def wait(self, predicate, timeout=8):
        deadline = time.monotonic() + timeout
        last = None
        while time.monotonic() < deadline:
            last = self.state()
            if predicate(last):
                return last
            time.sleep(0.025)
        raise AssertionError(f'bounded terminal predicate failed: {last!r}')

    def cursor(self, x, row):
        def matches(state):
            try:
                assert_cursor(state, x, row)
                return True
            except AssertionError:
                return False
        return self.wait(matches)

    def write(self, text):
        self.call('write', text)

    def key(self, *keys):
        self.call('key', 'press', *keys)

    def resize(self, cols, rows):
        self.call('resize', str(cols), str(rows))
        self.wait(lambda s: s['cols'] == cols and s['rows'] == rows)

    def text(self):
        result = self.call('text', '--full')
        value = json.loads(result.stdout)
        return value['data'] if isinstance(value['data'], str) else value['data']['text']

    def recording(self):
        value = self.call('get-recording', self.name).stdout
        (self.artifacts / 'output.cast').write_text(value)
        return output_bytes(value)

    def capture(self, label):
        for args, name in [(('state',), 'state.json'), (('text', '--full'), 'history.json'),
                           (('cells', '0', '0', '40', '12'), 'cells.json')]:
            try:
                value = self.call(*args, timeout=2).stdout
                (self.artifacts / f'{label}-{name}').write_text(value)
            except Exception as error:
                (self.artifacts / f'{label}-{name}.error').write_text(str(error))
        try:
            self.call('screenshot', str(self.artifacts / f'{label}.svg'), timeout=2)
            self.recording()
        except Exception as error:
            (self.artifacts / f'{label}-capture.error').write_text(str(error))

    def finish(self):
        self.key('ctrl+c')
        self.call('wait', 'exit', '--timeout', '5000')
        report = json.loads(self.report.read_text())
        assert report['SHA256'] == hashlib.sha256(report['Text'].encode()).hexdigest()
        self.capture('final')
        return report

    def close(self):
        ipc_errors = []
        for args in [('close',), ('daemon', 'stop')]:
            try:
                self.call(*args, allow=(3,), timeout=2)
            except Exception as error:
                ipc_errors.append(str(error))
        if ipc_errors and self.started:
            (self.artifacts / 'cleanup-ipc-errors.json').write_text(json.dumps(ipc_errors, indent=2) + '\n')
            for pid in (self.owner.get('shell_pid'), self.owner.get('pid')):
                self.terminate_owned(pid)
        result = self.call('daemon', 'status', allow=(3,), timeout=2)
        assert result.returncode == 3 and json.loads(result.stdout)['running'] is False


def g1(session):
    session.wait(lambda s: 'tail 2000' in s['text'], timeout=15)
    assert_transcript(session.text())
    session.write('draft')
    session.cursor(13, 'editor> draft')
    assert_no_replay(session.recording())
    report = session.finish()
    assert report['Text'] == 'draft' and report['Cursor'] == 5


def g2(session):
    session.write('draft')
    session.cursor(13, 'editor> draft')
    session.key('left', 'left', 'left', 'tab')
    session.wait(lambda s: 'item 01' in s['text'])
    session.key(*(['down'] * 11))
    session.cursor(0, '> item 12')
    repairs = session.recording().count('\x1b[2J\x1b[3J\x1b[H')
    session.resize(10, 12)
    wait_replay(session, repairs, 0)
    state = session.wait(lambda s: s['cursor'] == {'x': 0, 'y': s['text'].split('\n').index('> item 12')} if '> item 12' in s['text'].split('\n') and [r for r in s['text'].split('\n') if 'item ' in r] == ['  item 10', '  item 11', '> item 12'] else False)
    items = [line for line in state['text'].split('\n') if 'item ' in line]
    assert items == ['  item 10', '  item 11', '> item 12'], items
    assert 'editor>' not in state['text']
    session.write('ignored\x1b[200~ignored paste\x1b[201~')
    session.resize(40, 12)
    session.key('escape')
    state = session.cursor(10, 'editor> draft')
    assert 'item ' not in state['text']
    session.key('tab')
    session.cursor(0, '> item 12')
    session.key('escape')
    session.cursor(10, 'editor> draft')
    assert_resize_replay(session.recording(), 0)
    report = session.finish()
    assert report['Text'] == 'draft' and report['Cursor'] == 2
    assert report['Pastes'] == 0 and report['Submits'] == 0


def g3(session):
    session.wait(lambda s: 'tail 2000' in s['text'], timeout=15)
    assert_transcript(session.text())
    for width, height, x, row in [(13, 6, 3, '😀Z'), (12, 6, 5, '界😀Z'),
                                  (40, 12, 16, 'editor> abc界😀Z'),
                                  (40, 4, 16, 'editor> abc界😀Z'),
                                  (13, 6, 3, '😀Z'), (40, 12, 16, 'editor> abc界😀Z')]:
        repairs = session.recording().count('\x1b[2J\x1b[3J\x1b[H')
        session.resize(width, height)
        wait_replay(session, repairs)
        if width == 1:
            session.wait(lambda s: s['cursor'] == {'x': 0, 'y': 0})
        else:
            state = session.cursor(x, row)
            if width in (12, 13):
                first = 'editor> abc界' if width == 13 else 'editor> abc'
                assert_editor(state, first)
                cells = json.loads(session.call('cells', '0', '0', str(width), str(height)).stdout)['data']['cells']
                positions = {(cell['x'], cell['y']): cell['char'] for cell in cells}
                y = state['text'].split('\n').index(row)
                assert positions[(0, y)] == ('😀' if width == 13 else '界'), f'wide glyph start differs at {(0, y)}: {positions}'
                assert positions[(2, y)] == ('Z' if width == 13 else '😀'), f'wide glyph continuation differs at {(2, y)}: {positions}'
        assert_resize_replay(session.recording())
    assert_transcript(session.text())
    report = session.finish()
    assert report['Text'] == 'abc界😀Z'


def draft_resize(session, text, left):
    session.write('\x1b[200~' + text + '\x1b[201~')
    if left:
        session.key(*(['left'] * left))
    normalized = text.replace('\r\n', '\n')
    expected_cursor = len((normalized[:-left] if left else normalized).encode())
    for width, height in [(13, 6), (12, 6), (40, 12), (13, 6), (40, 4), (40, 12)]:
        repairs = session.recording().count('\x1b[2J\x1b[3J\x1b[H')
        session.resize(width, height)
        wait_replay(session, repairs, 0)
        if not left and width in (12, 13):
            if width == 13:
                final = '5界😀Z' if '012345' in text else ' 界😀Z'
                rows = ['fghijklmnopqr', 'stuvwxyz01234', final]
            else:
                final = '2345界😀Z' if '012345' in text else '234 界😀Z'
                rows = ['efghijklmnop', 'qrstuvwxyz01', final]
            state = session.cursor(6 if width == 13 else 9, final)
            start = state['text'].split('\n').index(rows[0])
            assert state['text'].split('\n')[start:start + 3] == rows, f'clipped draft rows differ: {state}'
            assert session.text().count('editor>') == 0, 'clipped editor prefix survived in history'
        else:
            state = session.wait(lambda s: s['text'].count('editor>') == 1)
            assert session.text().count('editor>') == 1, 'old editor rows survived in history'
        if width == 40:
            if left == 3:
                session.cursor(11, 'editor> abc界😀Z')
            elif left == 2:
                session.cursor(8, 'second界😀Z')
            else:
                session.cursor(5, '界😀Z')
        assert state['cursor']['x'] < width
    # Burst events must settle on the final size and preserve the same draft.
    repairs = session.recording().count('\x1b[2J\x1b[3J\x1b[H')
    for width, height in [(20, 8), (15, 7), (12, 6), (40, 12)]:
        session.call('resize', str(width), str(height))
    wait_replay(session, repairs, 0)
    assert session.text().count('editor>') == 1
    report = session.finish()
    assert report['Text'] == normalized and report['Cursor'] == expected_cursor
    assert report['Pastes'] == 1 and report['Submits'] == 0


def extreme_size(session):
    session.wait(lambda s: 'tail 2000' in s['text'], timeout=15)
    for width, height, x, row in [(13, 6, 3, '😀Z'), (12, 6, 5, '界😀Z'), (2, 1, 1, 'Z')]:
        session.resize(width, height)
        session.cursor(x, row)
    session.resize(1, 1)
    session.wait(lambda s: s['cursor'] == {'x': 0, 'y': 0})
    session.resize(40, 12)
    session.cursor(16, 'editor> abc界😀Z')
    assert_resize_replay(session.recording())
    session.finish()


def g6(session):
    state = session.wait(lambda s: 'Enter' in s['text'])
    supported = 'Shift/Ctrl/Alt+Enter' in state['text']
    session.write('a\x1b\rb\r')
    expected = 'a\nb'
    if supported:
        session.write('\x1b[13;2u\x1b[13;5u')
        expected += '\n\n'
    payload = '\r\n'.join(f'{i:04d}\t界😀\x1b[13;2u' for i in range(2000))
    session.write('\x1b[200~' + payload + '\x1b[201~')
    expected += payload.replace('\r\n', '\n')
    session.cursor(19, '1999    界😀?[13;2u')
    assert_no_replay(session.recording(), 0)
    report = session.finish()
    assert report['Text'] == expected, 'full normalized paste differs'
    assert report['Pastes'] == 1 and report['Submits'] == 1
    assert report['Lines'] == expected.count('\n') + 1
    assert report['Negotiated'] is supported
    return {'negotiated_modified_keys': 'pass' if supported else 'unsupported',
            'reason': None if supported else 'Terminal engine did not negotiate disambiguation; fallback passed.'}


def file_hashes(directory):
    return {str(path.relative_to(directory)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in sorted(directory.rglob('*')) if path.is_file()}


def product_hashes():
    paths = [ROOT / 'go.mod', ROOT / 'go.sum']
    for directory in ('cmd', 'internal', 'pkg', 'proto', 'migrations'):
        paths.extend(path for path in (ROOT / directory).rglob('*') if path.is_file())
    return {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in sorted(paths)}


def build_target(directory, artifacts):
    for source in ARCHIVE.glob('*.go'):
        shutil.copy2(source, directory / source.name)
    for name in ('go.mod', 'go.sum'):
        shutil.copy2(ARCHIVE / name, directory / name)
    shutil.copytree(ARCHIVE / 'evidence', directory / 'evidence')
    if (ARCHIVE / 'vendor').is_dir():
        shutil.copytree(ARCHIVE / 'vendor', directory / 'vendor')
    cache = Path(tempfile.gettempdir()) / 'askcore-tui-e2e-cache'
    env = dict(os.environ, GOWORK='off', GOCACHE=str(cache / 'go-build'),
               GOMODCACHE=os.environ.get('GOMODCACHE', str(cache / 'go-modules')))
    for args, log in [(['go', 'test', '-v', '-count=1', '-timeout', '60s', './...'], 'go-tests.txt'),
                      (['go', 'build', '-o', str(directory / 't0'), '.'], 'go-build.txt')]:
        with (artifacts / log).open('w') as output:
            result = subprocess.run(args, cwd=directory, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=90)
        assert result.returncode == 0, f'{args} failed; see {log}'
    log = (artifacts / 'go-tests.txt').read_text()
    selected = set(re.findall(r'^=== RUN   ([^/\n]+)$', log, re.M))
    expected = {name for source in ARCHIVE.glob('*_test.go')
                for name in re.findall(r'^func (Test\w+)\(t \*testing\.T\)', source.read_text(), re.M)}
    assert expected and expected <= selected, f'archived checks were not selected: {sorted(expected - selected)}'
    return directory / 't0'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--extreme-probe', action='store_true', help='Also run the known 1/2-column engine-limit probe')
    parser.add_argument('--target', type=Path, help='Existing T0-compatible binary; archived Go checks still run')
    parser.add_argument('--backend', action='append', choices=['alacritty', 'ghostty', 'rio'])
    parser.add_argument('--artifacts', type=Path)
    parser.add_argument('--case', action='append', choices=['g1', 'g2', 'g3', 'g3-draft', 'g3-pending', 'g3-capacity', 'g6'], help='Select focused cases; g3-capacity is an opt-in engine-limit probe')
    args = parser.parse_args()
    if not __debug__:
        parser.error('Run without -O/PYTHONOPTIMIZE; terminal assertions must stay enabled')
    cli = shutil.which('tui-test')
    if not cli:
        parser.error('tui-test is not installed')
    version = subprocess.run([cli, '--version'], capture_output=True, text=True, check=True).stdout.strip()
    expected = 'tui-test ' + (HERE / 'cli-version.txt').read_text().strip()
    if version != expected:
        parser.error(f'CLI pin mismatch: {version!r}, expected {expected!r}')
    run_id = uuid.uuid4().hex[:10]
    artifacts = (args.artifacts or HERE / 'artifacts' / run_id).resolve()
    artifacts.mkdir(parents=True)
    archive_expected = json.loads((ARCHIVE / 'archive-sha256.json').read_text())
    archive_before = file_hashes(ARCHIVE)
    for path, expected_hash in archive_expected.items():
        if archive_before.get(path) != expected_hash:
            parser.error(f'immutable archive hash mismatch: {path}')
    baseline_before = file_hashes(BASELINE)
    for path, expected_hash in json.loads((BASELINE / 'archive-sha256.json').read_text()).items():
        if baseline_before.get(path) != expected_hash:
            parser.error(f'immutable baseline hash mismatch: {path}')
    product_before = product_hashes()
    results = {'cli_version': version, 'headless_only': True, 'd14_ready': False, 'cases': []}
    try:
        with tempfile.TemporaryDirectory(prefix='askcore-tui-e2e-') as temporary:
            directory = Path(temporary)
            built = build_target(directory, artifacts)
            target = args.target.resolve() if args.target else built
            for backend in args.backend or ['alacritty', 'ghostty']:
                cases = [('g1', g1), ('g2', g2), ('g3', g3), ('g6', g6)]
                for label, draft, left in [('middle', 'abc界😀Z', 3),
                                           ('hard-lines', 'abc界😀Z\r\nsecond界😀Z', 2),
                                           ('long', 'abcdefghijklmnopqrstuvwxyz012345界😀Z', 0),
                                           ('trailing-space', 'abcdefghijklmnopqrstuvwxyz01234 界😀Z', 0)]:
                    cases.append((f'g3-draft-{label}', lambda s, text=draft, offset=left: draft_resize(s, text, offset)))
                cases.append(('g3-pending', None))
                if args.case and 'g3-capacity' in args.case:
                    cases.append(('g3-capacity', None))
                if args.extreme_probe:
                    cases.append(('g3-extreme', extreme_size))
                for scenario, test in cases:
                    group = 'g3-draft' if scenario.startswith('g3-draft-') else scenario if scenario in ('g3-pending', 'g3-capacity') else scenario.split('-')[0]
                    if args.case and group not in args.case:
                        continue
                    name = f'askcore-e2e-{run_id}-{backend}-{scenario}'
                    record = {'backend': backend, 'scenario': scenario, 'session': name, 'result': 'fail'}
                    if group in ('g3-pending', 'g3-capacity'):
                        record.pop('session')
                    session = None
                    try:
                        if group in ('g3-pending', 'g3-capacity'):
                            pending = runpy.run_path(str(HERE / 'pending-resize.py'))['pending_resize']
                            detail = pending(cli, backend, target, directory, artifacts / backend / scenario,
                                             capacity_probe=group == 'g3-capacity')
                        else:
                            fixture = 'g6' if group == 'g3-draft' else scenario.split('-')[0]
                            session = Session(cli, name, backend, target, directory, artifacts / backend / scenario, fixture)
                            detail = test(session)
                        record.update(result='pass', detail=detail)
                    except Exception as error:
                        record['error'] = str(error)
                        if session:
                            session.capture('failure')
                    finally:
                        if session:
                            try:
                                session.close()
                            except Exception as error:
                                record.update(result='fail', cleanup_error=str(error))
                        results['cases'].append(record)
                        (artifacts / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
                    print(f'{backend}/{scenario}: {record["result"]}', flush=True)
    except Exception as error:
        results['error'] = str(error)
    archive_after, product_after = file_hashes(ARCHIVE), product_hashes()
    baseline_after = file_hashes(BASELINE)
    boundaries = {'archive_before': archive_before, 'archive_after': archive_after,
                  'baseline_before': baseline_before, 'baseline_after': baseline_after,
                  'product_before': product_before, 'product_after': product_after}
    (artifacts / 'boundaries.json').write_text(json.dumps(boundaries, indent=2) + '\n')
    if archive_after != archive_before or baseline_after != baseline_before or product_after != product_before:
        results['boundary_error'] = 'Protected archive or product files changed during the run'
    (artifacts / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
    print(f'Artifacts: {artifacts}', flush=True)
    return 0 if results['cases'] and not results.get('error') and not results.get('boundary_error') and all(r['result'] == 'pass' for r in results['cases']) else 1


if __name__ == '__main__':
    sys.exit(main())
