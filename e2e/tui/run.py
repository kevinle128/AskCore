#!/usr/bin/env python3
"""Run the archived T0 binary and the built `ask` through owned tui-test sessions."""
import argparse
from collections import Counter
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


def hide_typed_secret(recording, secret=None):
    """The recording also holds the input events, which are what the runner typed.

    The fake key of the `ask-providers` case is replaced in those events only. An output
    event is kept as it is, so a key that the product wrote still shows up in the scan.
    """
    secret = ASK_FAKE_KEY if secret is None else secret
    lines = recording.splitlines()
    if not lines:
        return recording
    out = [lines[0]]
    for line in lines[1:]:
        try:
            event = json.loads(line)
        except ValueError:
            out.append(line)
            continue
        if isinstance(event, list) and len(event) > 2 and event[1] == 'i' and isinstance(event[2], str):
            event[2] = event[2].replace(secret, '<secret>')
            line = json.dumps(event, ensure_ascii=False)
        out.append(line)
    return '\n'.join(out) + ('\n' if recording.endswith('\n') else '')


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
    def __init__(self, cli, name, backend, target, directory, artifacts, scenario, *, cols=40, rows=12,
                 env=None, env_names=(), verbose=True, ready=None, ready_timeout=8):
        # The defaults are the T0 contract. The `ask` driver passes its own size, a
        # process environment, no scenario and its own ready check.
        self.cli, self.name, self.backend = cli, name, backend
        self.artifacts = artifacts
        self.size = (cols, rows)
        self.env_names = tuple(env_names)
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
            if scenario is None:
                tail = []
                term = []
            else:
                tail = ['--scenario', scenario, '--draft-report', str(self.report)]
                term = ['--env', 'TERM=xterm-256color']
            result = self.call(*(['--verbose'] if verbose else []), 'run', '--backend', backend,
                               '--cols', str(cols), '--rows', str(rows),
                               '--config', str(HERE / 'tui-test.toml'),
                               '--cwd', str(directory), *term, str(target), *tail, env=env)
            self.started = True
            self.owner = json.loads(result.stdout)['data']
            self.identities = {pid: self.identity(pid) for pid in (self.owner['pid'], self.owner['shell_pid'])}
            (artifacts / 'ownership.json').write_text(json.dumps(self.owner, indent=2) + '\n')
            self.wait(ready or (lambda s: 'editor>' in s['text']), timeout=ready_timeout)
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

    def call(self, *args, allow=(), timeout=8, env=None, secret=''):
        """Run one CLI call. A fake secret that the call types is kept out of the records
        of the call, so that the artifact scan can look for it in what the product wrote."""
        command = [self.cli, '--session', self.name, '--json', *args]

        def hide(text):
            return text.replace(secret, '<secret>') if secret else text

        shown = [hide(part) for part in command]
        try:
            result = subprocess.run(command, capture_output=True, text=True, timeout=timeout, env=env)
            recorded = hide_typed_secret(result.stdout) if args and args[0] == 'get-recording' else hide(result.stdout)
            entry = {'command': shown, 'code': result.returncode, 'stdout': recorded, 'stderr': hide(result.stderr)}
            if env is not None:
                # Names only: values can hold secrets that the inherited environment carries.
                entry['environment_names'] = sorted(self.env_names)
        except subprocess.TimeoutExpired as error:
            self.calls.append({'command': shown, 'timeout': timeout})
            (self.artifacts / 'commands.json').write_text(json.dumps(self.calls, ensure_ascii=False, indent=2) + '\n')
            raise RuntimeError(f'CLI timed out after {timeout}s: {[hide(str(a)) for a in args]}') from error
        self.calls.append(entry)
        (self.artifacts / 'commands.json').write_text(json.dumps(self.calls, ensure_ascii=False, indent=2) + '\n')
        if result.returncode not in (0, *allow):
            raise RuntimeError(hide(f'{args}: exit {result.returncode}: {result.stderr}{result.stdout}'))
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

    def write(self, text, secret=''):
        self.call('write', text, secret=secret)

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
        (self.artifacts / 'output.cast').write_text(hide_typed_secret(value))
        return output_bytes(value)

    def capture(self, label):
        for args, name in [(('state',), 'state.json'), (('text', '--full'), 'history.json'),
                           (('cells', '0', '0', *map(str, self.size)), 'cells.json')]:
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
        # Tests, testdata and docs never change the binary under test, and
        # developers edit them while a long run is in progress.
        paths.extend(path for path in (ROOT / directory).rglob('*')
                     if path.is_file() and path.suffix != '.md'
                     and not path.name.endswith('_test.go')
                     and 'testdata' not in path.parts)
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


def run_t0(args, cli, run_id, directory, artifacts, results):
    """Run the archived T0 gate scenarios; these scenarios and their assertions are frozen."""
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


# Built `ask` driver
#
# The driver starts the real `ask` with no arguments in an owned tui-test session.
# The TUI starts its own leader in an isolated ASK_HOME. Model replies come from the
# faux provider, which the leader lists only with ASK_FAUX=1. The runner sets that,
# the paced reply speed and the home, and removes every credential variable from
# the child environment, so no case can reach a billed provider.

ASK_CASES = ['ask-idle', 'ask-selector', 'ask-resize', 'ask-providers']
# A fake key that the `ask-providers` case pastes into the key box. It is new for each
# run, it is never a real key, and the artifact scan looks for it in what the product
# wrote: the screen, the scrollback, the recording and the cell dumps.
ASK_FAKE_KEY = 'sk-fake-runner-' + uuid.uuid4().hex
PROVIDER_NAMES = ['Alibaba Token Plan API key', 'Claude Pro/Max sign-in', 'Anthropic API key',
                  'ChatGPT Plus/Pro sign-in', 'OpenAI API key', 'xAI Grok sign-in', 'xAI API key']
PROVIDER_HINT = 'enter sign in  ·  esc close  ·  your draft is kept'
PROVIDER_ROW = re.compile(r'^│ [❯ ] ● (.*?)\s*│$')
STREAM_MARKERS = 100
STREAM_PROMPT = ' '.join(f'm{i:03d}' for i in range(STREAM_MARKERS))
LEADER_FOOTER = re.compile(r'● leader [0-9a-f]+')
USAGE_ROW = re.compile(r'^\s+faux-1 · [\d.]+s · ↑\d+ ↓\d+$', re.M)
PURGE = '\x1b[2J\x1b[3J\x1b[H'
ALT_SCREEN = ('\x1b[?47h', '\x1b[?1047h', '\x1b[?1049h')
SECRET_NAME = re.compile(r'API[_-]?KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|PRIVATE[_-]?KEY|AUTH', re.I)
NOT_SECRET_NAME = re.compile(r'(_URL|_SOCK|_PATH|_DIR|_FILE|_HOST)$', re.I)
ASK_COLUMNS = 80
ASK_ROWS = 24
STREAM_TPS = 20


def ask_ready(state):
    """The leader footer is shown and the in-process fallback is not."""
    text = state['text']
    return '❯ Ask anything' in text and LEADER_FOOTER.search(text) is not None and 'in-process' not in text


def editor_rows(text):
    return [i for i, line in enumerate(text.split('\n')) if line.startswith('│ ❯ ')]


def assert_frame(state, columns):
    """One startup header, one editor box with full-width borders, a leader footer."""
    lines = state['text'].split('\n')
    assert sum(line.startswith('◢◤ ask') for line in lines) == 1, f'header count differs: {state["text"]!r}'
    tops = [i for i, line in enumerate(lines) if line.startswith('╭')]
    bottoms = [i for i, line in enumerate(lines) if line.startswith('╰')]
    assert len(tops) == 1 and len(bottoms) == 1, f'expected one editor box, got tops={tops} bottoms={bottoms}'
    assert bottoms[0] > tops[0], 'editor box is upside down'
    for row in range(tops[0], bottoms[0] + 1):
        assert len(lines[row]) == columns, f'editor box row {row} is {len(lines[row])} columns, expected {columns}'
    footer = lines[bottoms[0] + 1]
    assert LEADER_FOOTER.search(footer), f'leader id missing in footer: {footer!r}'
    assert 'faux-1' in footer and 'in-process' not in state['text'], f'unexpected footer: {footer!r}'
    return tops[0], bottoms[0]


def assert_idle_screen(state, columns):
    top, bottom = assert_frame(state, columns)
    assert bottom == top + 2, 'idle editor must be one row'
    row = state['text'].split('\n')[top + 1]
    assert row.startswith('│ ❯ Ask anything  ·  / commands  ·  ctrl+l model'), f'placeholder differs: {row!r}'
    assert state['cursor'] == {'x': 4, 'y': top + 1}, f'cursor={state["cursor"]}'


def assert_selector_screen(state, columns):
    lines = state['text'].split('\n')
    titles = [i for i, line in enumerate(lines) if line.startswith('╭─ Select model ')]
    assert len(titles) == 1, f'expected one selector: {state["text"]!r}'
    assert not any(line.startswith('│ ❯ draft') for line in lines), 'selector must replace the editor'
    # The leader lists usable models grouped by provider: a header row for the
    # provider and the model row under it.
    current = [i for i, line in enumerate(lines)
               if line.startswith('│ ❯   faux-1') and re.search(r'current\s+│$', line)]
    assert len(current) == 1, f'current model row differs: {lines!r}'
    assert lines[current[0] - 1].startswith('│   ● faux'), f'provider header differs: {lines!r}'
    hint = [line for line in lines if line.strip() == 'enter select  ·  esc close  ·  your draft is kept']
    assert len(hint) == 1, f'selector hint differs: {lines!r}'
    row = titles[0]
    while row < len(lines) and not lines[row].startswith('╰'):
        assert len(lines[row]) == columns, f'selector row {row} is {len(lines[row])} columns, expected {columns}'
        row += 1
    assert row < len(lines), 'selector has no bottom border'
    assert any(LEADER_FOOTER.search(line) for line in lines), 'leader footer missing'


def provider_box(lines):
    """Return (title row, bottom row) of the provider box, or raise."""
    titles = [i for i, line in enumerate(lines) if line.startswith('╭─ Providers ')]
    assert len(titles) == 1, f'expected one provider box: {lines!r}'
    bottom = next((i for i in range(titles[0], len(lines)) if lines[i].startswith('╰')), None)
    assert bottom is not None, 'provider box has no bottom border'
    return titles[0], bottom


def provider_names(lines):
    """Names of the provider rows in the box, without the status text at the right."""
    top, bottom = provider_box(lines)
    names = []
    for line in lines[top + 1:bottom]:
        match = PROVIDER_ROW.match(line)
        if match:
            text = match.group(1)
            # The status text is separated from the name by two or more spaces.
            names.append(re.split(r'\s{2,}', text)[0])
    return names


def assert_provider_box(state, columns, names=PROVIDER_NAMES):
    """The box has the rows in order, the search row last, full-width rows and the hint."""
    lines = state['text'].split('\n')
    top, bottom = provider_box(lines)
    for row in range(top, bottom + 1):
        assert len(lines[row]) == columns, f'provider box row {row} is {len(lines[row])} columns, expected {columns}'
    assert not any(line.startswith('│ ❯ Ask anything') for line in lines), 'the box must replace the editor'
    assert provider_names(lines) == names, f'provider rows differ: {provider_names(lines)!r}'
    assert '⌕' in lines[bottom - 1], f'the search row is not the last row: {lines[bottom - 1]!r}'
    assert lines[bottom + 1].strip() == PROVIDER_HINT, f'hint differs: {lines[bottom + 1]!r}'
    assert any(LEADER_FOOTER.search(line) for line in lines), 'leader footer missing'


def assert_stream_text(text, columns, markers=STREAM_MARKERS, complete=True):
    """The full text holds the prompt once and the reply once, each word in order.

    While the reply streams (complete is false) the reply may stop at any word.
    """
    lines = text.split('\n')
    assert sum(line.startswith('◢◤ ask') for line in lines) == 1, 'startup header is missing or drawn twice'
    assert sum(line.startswith('  ❯ m000') for line in lines) == 1, 'prompt band is missing or drawn twice'
    assert sum(line.startswith('  ◆ m000') for line in lines) == 1, 'reply start is missing or drawn twice'
    found = re.findall(r'\bm\d{3}\b', text)
    expected = [f'm{i:03d}' for i in range(markers)]
    if complete:
        assert found == expected * 2, f'ordered prompt and reply differ: total={len(found)}, first words={found[:3]}'
    else:
        assert found[:markers] == expected and found[markers:] == expected[:len(found) - markers], \
            f'ordered prompt and partial reply differ: total={len(found)}'
    too_wide = [line for line in lines if len(line) > columns]
    assert not too_wide, f'rows wider than {columns} columns survived: {too_wide[:2]!r}'


def assert_rebuild_once(raw, markers=STREAM_MARKERS):
    """One purge and rebuild for the resize, and the rebuilt prompt starts in order after it."""
    parts = raw.split(PURGE)
    assert len(parts) == 2, f'expected one rebuild after the resize, got {len(parts) - 1}'
    plain = re.sub(r'\x1b\[[0-9;?]*[A-Za-z]', '', parts[1])
    found = re.findall(r'\bm\d{3}\b', plain)
    assert found[:markers] == [f'm{i:03d}' for i in range(markers)], 'rebuilt prompt is incomplete or out of order'
    assert plain.count('◢◤ ask') == 1, 'startup header missing or drawn twice in the rebuild'


def assert_one_draft(state, draft):
    """The draft is in the editor once and the cursor sits after its last character."""
    rows = [i for i, line in enumerate(state['text'].split('\n')) if line.startswith(f'│ ❯ {draft}')]
    assert len(rows) == 1, f'expected one editor row with the draft: {state["text"]!r}'
    assert state['text'].count(draft) == 1, 'draft text is drawn more than once'
    expected = {'x': 4 + len(draft), 'y': rows[0]}
    assert state['cursor'] == expected, f'cursor={state["cursor"]}, expected={expected}'


def wait_one_draft(session, draft):
    """Wait for the draft and cursor to settle; a repaint in progress can show a moved cursor."""
    def settled(state):
        try:
            assert_one_draft(state, draft)
            return True
        except AssertionError:
            return False
    try:
        return session.wait(settled)
    except AssertionError:
        assert_one_draft(session.state(), draft)
        raise


def assert_no_alt_screen(raw):
    for sequence in ALT_SCREEN:
        assert sequence not in raw, f'alternate screen entered: {sequence!r}'


def secret_values(environ, minimum=8):
    """Variable name to value for names that look like credentials."""
    return {name: value for name, value in environ.items()
            if SECRET_NAME.search(name) and not NOT_SECRET_NAME.search(name) and len(value) >= minimum}


def scan_secrets(directory, secrets):
    """Return {relative path: [variable names]} for files that hold a secret value.

    A matching file is removed, so the evidence that is kept never holds a value.
    Only the variable names leave this function.
    """
    needles = {name: value.encode() for name, value in secrets.items()}
    hits = {}
    for path in sorted(Path(directory).rglob('*')):
        if not path.is_file():
            continue
        data = path.read_bytes()
        names = [name for name, needle in needles.items() if needle in data]
        if names:
            hits[str(path.relative_to(directory))] = names
            path.unlink()
    return hits


def is_credential_name(name):
    """True for a variable whose name looks like a credential, of any length."""
    return bool(SECRET_NAME.search(name)) and not NOT_SECRET_NAME.search(name)


def ask_environment(home, environ=None):
    """The process environment of one `ask` case.

    ASK_* settings of the caller are dropped so that a local configuration cannot
    change the run, and so are the variables that look like credentials, so that no
    case can sign in to a real provider. The faux provider is enabled on purpose.
    """
    environ = os.environ if environ is None else environ
    env = {name: value for name, value in environ.items()
           if not name.startswith('ASK_') and not is_credential_name(name)}
    env.update(ASK_HOME=str(home), ASK_FAUX='1', ASK_FAUX_TPS=str(STREAM_TPS), TERM='xterm-256color', LANG='en_US.UTF-8')
    return env


ASK_ENV_NAMES = ('ASK_HOME', 'ASK_FAUX', 'ASK_FAUX_TPS', 'TERM', 'LANG')


def process_table():
    result = subprocess.run(['ps', '-axo', 'pid=,command='], capture_output=True, text=True, timeout=5)
    rows = []
    for line in result.stdout.splitlines():
        fields = line.strip().split(None, 1)
        if len(fields) == 2 and fields[0].isdigit():
            rows.append((int(fields[0]), fields[1]))
    return rows


def leader_processes(binary):
    """Leaders of this run. The binary path is unique to the run, so no other leader matches."""
    return [(pid, command) for pid, command in process_table() if command.startswith(f'{binary} leader')]


def stop_leader(binary, home, env, artifacts):
    """Stop the leader that the TUI started in this home, then check that none is left."""
    errors = []
    try:
        result = subprocess.run([str(binary), 'leader', 'stop'], env=env, capture_output=True, text=True, timeout=20)
        (artifacts / 'leader-stop.txt').write_text(result.stdout + result.stderr)
    except Exception as error:
        errors.append(f'leader stop failed: {error}')
    deadline = time.monotonic() + 5
    while leader_processes(binary) and time.monotonic() < deadline:
        time.sleep(0.05)
    for pid, command in leader_processes(binary):
        # Only a process that runs the binary of this run is signaled, and the command is
        # checked again before each signal so that a reused PID is never hit.
        errors.append(f'leader {pid} ignored leader stop')
        for sig in (signal.SIGTERM, signal.SIGKILL):
            if (pid, command) not in leader_processes(binary):
                break
            os.kill(pid, sig)
            time.sleep(0.5)
    left = leader_processes(binary)
    if left:
        errors.append(f'leader processes left: {left}')
    log = Path(home) / 'leader.log'
    if log.is_file():
        shutil.copy2(log, artifacts / 'leader.log')
    if errors:
        raise RuntimeError('; '.join(errors))


class AskSession(Session):
    """A session of the built `ask`; the terminal size is tracked for the cell dumps."""

    def resize(self, cols, rows):
        super().resize(cols, rows)
        self.size = (cols, rows)

    def quit(self):
        # ctrl+d quits only on an empty draft; with a draft it deletes a character.
        # ctrl+c clears a draft first.
        if not any(line.startswith('│ ❯ Ask anything') for line in self.state()['text'].split('\n')):
            self.key('ctrl+c')
            self.wait(lambda s: any(line.startswith('│ ❯ Ask anything') for line in s['text'].split('\n')))
        self.key('ctrl+d')
        self.call('wait', 'exit', '--timeout', '10000', timeout=15)
        self.capture('final')


def ask_idle(session):
    # The header is committed after the first frame and the model name comes
    # with the state answer, so wait until the idle screen is complete.
    def idle(state):
        try:
            assert_idle_screen(state, ASK_COLUMNS)
            return True
        except AssertionError:
            return False
    session.wait(idle)
    state = session.state()
    assert_idle_screen(state, ASK_COLUMNS)
    assert_no_alt_screen(session.recording())
    session.capture('idle')
    session.quit()
    return {'screen': 'idle', 'columns': ASK_COLUMNS, 'rows': ASK_ROWS}


def ask_selector(session):
    session.write('draft')
    session.wait(lambda s: any(line.startswith('│ ❯ draft') for line in s['text'].split('\n')))
    session.key('ctrl+l')
    state = session.wait(lambda s: 'Select model' in s['text'])
    assert_selector_screen(state, ASK_COLUMNS)
    session.capture('selector')
    session.key('escape')
    session.wait(lambda s: 'Select model' not in s['text'])
    wait_one_draft(session, 'draft')
    assert_no_alt_screen(session.recording())
    session.quit()
    return {'screen': 'selector', 'draft_kept': True}


def ask_resize(session):
    """Resize from 120x40 to 80x24 while a reply streams, with a draft in the editor."""
    session.write(STREAM_PROMPT)
    session.key('enter')
    session.wait(lambda s: '◆ m000' in s['text'] and 'Working…' in s['text'], timeout=20)
    session.write('draft')
    session.wait(lambda s: any(line.startswith('│ ❯ draft') for line in s['text'].split('\n')))
    assert 'Working…' in session.state()['text'], 'the reply ended before the resize'
    session.resize(ASK_COLUMNS, ASK_ROWS)
    assert not USAGE_ROW.search(session.state()['text']), 'the reply ended before the resize settled'
    wait_one_draft(session, 'draft')
    # The renderer rebuilds the scrollback 120 ms after the size stopped changing.
    session.wait(lambda s: session.recording().count(PURGE) == 1)
    state = wait_one_draft(session, 'draft')
    assert 'Working…' in state['text'], 'the reply ended before the rebuild was checked'
    assert_stream_text(session.text(), ASK_COLUMNS, complete=False)
    session.wait(lambda s: USAGE_ROW.search(s['text']) is not None, timeout=40)
    wait_one_draft(session, 'draft')
    assert_stream_text(session.text(), ASK_COLUMNS)
    raw = session.recording()
    assert_no_alt_screen(raw)
    assert_rebuild_once(raw)
    session.capture('resized')
    session.quit()
    return {'resize': '120x40 to 80x24 while streaming', 'draft_and_cursor_kept': True}


def ask_providers(session):
    """Open the provider box, filter it, open a key box, paste a fake key and go back."""
    session.write('/providers')
    session.key('enter')
    state = session.wait(lambda s: 'Type to search' in s['text'] and
                         all(name in s['text'] for name in PROVIDER_NAMES))
    assert_provider_box(state, ASK_COLUMNS)
    session.capture('providers')

    # The filter keeps the rows with the typed text, and enter on a row with no
    # saved credential opens its key box at once.
    session.write('key')
    state = session.wait(lambda s: '⌕ key' in s['text'])
    assert provider_names(state['text'].split('\n')) == [n for n in PROVIDER_NAMES if 'key' in n], state['text']
    session.key('ctrl+u')
    session.wait(lambda s: 'Type to search' in s['text'])
    session.write('openai api')
    session.wait(lambda s: '⌕ openai api' in s['text'])
    session.key('enter')
    state = session.wait(lambda s: 'Paste your API key.' in s['text'])
    lines = state['text'].split('\n')
    assert any(line.startswith('│  key  paste or type the key; it stays hidden') for line in lines), state['text']
    assert not any(line.startswith('│ ❯ Ask anything') for line in lines), 'the key box must replace the editor'

    # The pasted key shows as (hidden) and is on no row of the screen or the scrollback.
    session.write('\x1b[200~' + ASK_FAKE_KEY + '\x1b[201~', secret=ASK_FAKE_KEY)
    state = session.wait(lambda s: any(line.startswith('│  key  (hidden)') for line in s['text'].split('\n')))
    assert ASK_FAKE_KEY not in state['text'] and ASK_FAKE_KEY not in session.text(), 'the key is on the screen'
    session.capture('key-box')

    # esc cancels the key box and brings the editor back; the key is dropped.
    session.key('escape')
    session.wait(lambda s: 'Paste your API key.' not in s['text'] and
                 any(line.startswith('│ ❯ Ask anything') for line in s['text'].split('\n')))
    assert ASK_FAKE_KEY not in session.text()
    raw = session.recording()
    assert ASK_FAKE_KEY not in raw, 'the key was written to the terminal'
    assert_no_alt_screen(raw)
    session.capture('closed')
    session.quit()
    return {'screen': 'providers', 'rows': len(PROVIDER_NAMES), 'key_box': True, 'secret_on_screen': False}


def compile_ask(base, artifacts):
    binary = base / 'ask'
    with (artifacts / 'go-compile.txt').open('w') as output:
        result = subprocess.run(['go', 'build', '-o', str(binary), './cmd/tui'], cwd=ROOT,
                                env=dict(os.environ, GOWORK='off'), stdout=output, stderr=subprocess.STDOUT, timeout=600)
    assert result.returncode == 0, 'go build of ./cmd/tui failed; see go-compile.txt'
    return binary


def run_ask(args, cli, run_id, artifacts, results):
    """Run the `ask` scenarios; each case has its own ASK_HOME and its own leader."""
    selected = [name for name in ASK_CASES if not args.case or name in args.case]
    if not selected:
        return
    # A Unix socket path is short-limited, so the base is under /tmp and not the user temp dir.
    base = Path(tempfile.mkdtemp(prefix='askl-', dir='/tmp')).resolve()
    secrets = secret_values(os.environ)
    environment = {'set_names': list(ASK_ENV_NAMES), 'dropped_prefix': 'ASK_',
                   'dropped_credential_names': sorted(name for name in os.environ if is_credential_name(name))}
    (artifacts / 'ask-environment.json').write_text(json.dumps(environment, indent=2) + '\n')
    try:
        binary = compile_ask(base, artifacts)
        cases = {'ask-idle': (ask_idle, ASK_COLUMNS, ASK_ROWS), 'ask-selector': (ask_selector, ASK_COLUMNS, ASK_ROWS),
                 'ask-resize': (ask_resize, 120, 40), 'ask-providers': (ask_providers, ASK_COLUMNS, ASK_ROWS)}
        for backend in args.backend or ['alacritty', 'ghostty']:
            for scenario in selected:
                test, cols, rows = cases[scenario]
                name = f'askcore-e2e-{run_id}-ask-{backend}-{scenario}'
                record = {'driver': 'ask', 'backend': backend, 'scenario': scenario, 'session': name, 'result': 'fail'}
                home = base / f'{backend}-{scenario}'
                work = base / f'work-{backend}-{scenario}'
                work.mkdir()
                env = ask_environment(home)
                case_dir = artifacts / 'ask' / backend / scenario
                session = None
                try:
                    session = AskSession(cli, name, backend, binary, work, case_dir, None, cols=cols, rows=rows, env=env,
                                         env_names=ASK_ENV_NAMES, verbose=False, ready=ask_ready, ready_timeout=30)
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
                    try:
                        stop_leader(binary, home, env, case_dir)
                    except Exception as error:
                        record.update(result='fail', leader_cleanup_error=str(error))
                    results['cases'].append(record)
                    (artifacts / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
                print(f'ask/{backend}/{scenario}: {record["result"]}', flush=True)
        left = leader_processes(binary)
        assert not left, f'leader processes left after the run: {left}'
    finally:
        shutil.rmtree(base, ignore_errors=True)


def select_drivers(parser, args):
    t0_cases = {'g1', 'g2', 'g3', 'g3-draft', 'g3-pending', 'g3-capacity', 'g6'}
    if args.driver:
        chosen = list(dict.fromkeys(args.driver))
    elif args.case:
        chosen = [d for d, cases in (('t0', t0_cases), ('ask', set(ASK_CASES))) if cases & set(args.case)]
    else:
        chosen = ['t0', 'ask']
    for case in args.case or []:
        if case in ASK_CASES and 'ask' not in chosen or case not in ASK_CASES and 't0' not in chosen:
            parser.error(f'case {case} does not belong to the selected driver')
    return chosen


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--extreme-probe', action='store_true', help='Also run the known 1/2-column engine-limit probe')
    parser.add_argument('--target', type=Path, help='Existing T0-compatible binary; archived Go checks still run')
    parser.add_argument('--backend', action='append', choices=['alacritty', 'ghostty', 'rio'])
    parser.add_argument('--artifacts', type=Path)
    parser.add_argument('--driver', action='append', choices=['t0', 'ask'], help='t0: archived T0 gate scenarios; ask: the built ask. Default: both, or the driver that owns the selected cases')
    parser.add_argument('--case', action='append', choices=['g1', 'g2', 'g3', 'g3-draft', 'g3-pending', 'g3-capacity', 'g6', *ASK_CASES], help='Select focused cases; g3-capacity is an opt-in engine-limit probe; ask-* cases use the ask driver')
    args = parser.parse_args()
    drivers = select_drivers(parser, args)
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
            for driver, run in (('t0', lambda: run_t0(args, cli, run_id, directory, artifacts, results)),
                                ('ask', lambda: run_ask(args, cli, run_id, artifacts, results))):
                if driver not in drivers:
                    continue
                try:
                    run()
                except Exception as error:
                    results.setdefault('driver_errors', {})[driver] = str(error)
                    results['error'] = results.get('error', '') + f'{driver}: {error}\n'
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
    # Evidence is kept only when no credential value from the inherited environment is in it.
    leaked = scan_secrets(artifacts, {**secret_values(os.environ), 'ASK_FAKE_KEY': ASK_FAKE_KEY})
    if leaked:
        results = {'secret_error': 'credential values found in artifacts; the files were removed',
                   'files': sorted(leaked), 'variables': sorted({n for names in leaked.values() for n in names}),
                   'cases': [{'scenario': r['scenario'], 'result': 'fail'} for r in results['cases']]}
        (artifacts / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
        print(f'Credential values found in {len(leaked)} artifact file(s); removed. Variables: {results["variables"]}', flush=True)
    print(f'Artifacts: {artifacts}', flush=True)
    return 0 if not leaked and results['cases'] and not results.get('error') and not results.get('boundary_error') and all(r['result'] == 'pass' for r in results['cases']) else 1


if __name__ == '__main__':
    sys.exit(main())
