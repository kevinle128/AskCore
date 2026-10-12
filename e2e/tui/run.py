#!/usr/bin/env python3
"""Run the built `ask` through owned tui-test sessions on real terminal engines.

The interactive `ask` TUI uses the alternate screen: a header row, a block pane,
a prompt pane between two rules and a shortcut bar. Each case starts the
program with no arguments in a session, drives it with keys and SGR mouse
input, and checks the engine screen and the output recording.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


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


class Session:
    """One owned tui-test session: its CLI calls, process identity and cleanup."""

    def __init__(self, cli, name, backend, target, directory, artifacts, *, cols, rows, env=None, env_names=(),
                 ready, ready_timeout=30):
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
        status = self.call('daemon', 'status', allow=(3,))
        assert status.returncode == 3, 'refusing to reuse an existing daemon'
        try:
            result = self.call('run', '--backend', backend, '--cols', str(cols), '--rows', str(rows),
                               '--config', str(HERE / 'tui-test.toml'), '--cwd', str(directory),
                               str(target), env=env)
            self.started = True
            self.owner = json.loads(result.stdout)['data']
            self.identities = {pid: self.identity(pid) for pid in (self.owner['pid'], self.owner['shell_pid'])}
            (artifacts / 'ownership.json').write_text(json.dumps(self.owner, indent=2) + '\n')
            self.wait(ready, timeout=ready_timeout)
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

    def write(self, text, secret=''):
        self.call('write', text, secret=secret)

    def key(self, *keys):
        self.call('key', 'press', *keys)

    def resize(self, cols, rows):
        self.call('resize', str(cols), str(rows))
        self.wait(lambda s: s['cols'] == cols and s['rows'] == rows)
        self.size = (cols, rows)

    def text_full(self):
        """The screen text. The alternate screen has no scrollback, so this is the visible frame."""
        value = json.loads(self.call('text', '--full').stdout)['data']
        return value if isinstance(value, str) else value['text']

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


def product_hashes():
    """Hashes of the files that make the binary under test. The run fails when they change."""
    paths = [ROOT / 'go.mod', ROOT / 'go.sum']
    for directory in ('cmd', 'internal', 'pkg', 'proto', 'migrations', 'third_party'):
        # Tests, testdata and docs never change the binary under test, and
        # developers edit them while a long run is in progress.
        paths.extend(path for path in (ROOT / directory).rglob('*')
                     if path.is_file() and path.suffix != '.md'
                     and not path.name.endswith('_test.go')
                     and 'testdata' not in path.parts)
    return {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in sorted(paths)}


# Cases of the built `ask`
#
# The TUI starts its own leader in an isolated ASK_HOME. Model replies come from the
# faux provider, which the leader lists only with ASK_FAUX=1. The runner sets that,
# the paced reply speed and the home, and removes every credential variable from
# the child environment, so no case can reach a billed provider.

ASK_CASES = ['ask-idle', 'ask-turn', 'ask-resize', 'ask-selector', 'ask-providers', 'ask-mouse',
             'ask-exit-quit', 'ask-exit-ctrl-c', 'ask-exit-sigterm', 'ask-exit-sighup',
             'ask-copy', 'ask-in-process']
# Cases that run with the in-process agent: ASK_HOME is a symlink, which the leader
# connect refuses as an unsafe endpoint, so the TUI falls back at startup and starts no leader.
IN_PROCESS_CASES = {'ask-in-process'}
# Cases that run as if over SSH, so /copy skips the native clipboard command and uses OSC 52.
REMOTE_CASES = {'ask-copy'}
REMOTE_ENV = {'SSH_TTY': '/dev/pts/0'}
OSC52 = '\x1b]52;c;'
OSC52_SEQUENCE = re.compile(r'\x1b\]52;c;([A-Za-z0-9+/=]*)(?:\x07|\x1b\\)')
# A fake key that the `ask-providers` case pastes into the key box. It is new for each
# run, it is never a real key, and the artifact scan looks for it in what the product
# wrote: the screen, the recording and the cell dumps.
ASK_FAKE_KEY = 'sk-fake-runner-' + uuid.uuid4().hex
PROVIDER_NAMES = ['Alibaba Token Plan API key', 'Claude Pro/Max sign-in', 'Anthropic API key',
                  'ChatGPT Plus/Pro sign-in', 'OpenAI API key', 'xAI Grok sign-in', 'xAI API key']
PROVIDER_HINT = 'enter sign in  ·  esc close  ·  your draft is kept'
SELECTOR_HINT = 'enter select  ·  esc close  ·  your draft is kept'
PROVIDER_ROW = re.compile(r'^│ [❯ ] ● (.*?)\s*│$')
STREAM_MARKERS = 100
STREAM_PROMPT = ' '.join(f'm{i:03d}' for i in range(STREAM_MARKERS))
LEADER_LABEL = re.compile(r'● leader [0-9a-f]+')
HEADER_LONG = re.compile(r'^◢◤ ask  \S+ leader [0-9a-f]+')
HEADER_SHORT = re.compile(r'^◢◤ ask \S+ [0-9a-f]+')
HEADER_IN_PROCESS = re.compile(r'^◢◤ ask  \S+ in-process')
SHORT_WIDTH = 60
# The usage row of a reply. Token counts of 1000 and more are written as 2.6k or 1.2M.
USAGE_ROW = re.compile(r'^\s+faux-1(?: · [\d.]+s)? · ↑[\d.]+[kM]? ↓[\d.]+[kM]?$', re.M)
PURGE = '\x1b[3J'
ALT_ON, ALT_OFF = '\x1b[?1049h', '\x1b[?1049l'
MODE = re.compile(r'\x1b\[\?([0-9;]+)([hl])')
MOUSE_MODES = ('1000', '1002', '1003', '1006')
PASTE_BEGIN, PASTE_END = '\x1b[200~', '\x1b[201~'
BAR_PROMPT = ' Tab blocks'
BAR_LIST = ' j/k'
VIEWER_BAR = ' ↑↓ scroll · PgUp PgDn page · Home End · Esc back'
END_MARKER = re.compile(r'^\s*(▼ End|▼ \d+ new lines · End)\s*$')
EXIT_LINE = re.compile(r'ask: session \S+')
SECRET_NAME = re.compile(r'API[_-]?KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|PRIVATE[_-]?KEY|AUTH', re.I)
NOT_SECRET_NAME = re.compile(r'(_URL|_SOCK|_PATH|_DIR|_FILE|_HOST)$', re.I)
ASK_COLUMNS = 80
ASK_ROWS = 24
STREAM_TPS = 20
# A prompt that calls the echo tool with nine lines. The faux reply says the tool
# output back as one paragraph, so an output line alone on a row is tool output.
ECHO_LINES = [f'out-{i:02d}' for i in range(1, 10)]
ECHO_PROMPT = 'echo ' + '\n'.join(ECHO_LINES)
ECHO_REPLY = ' '.join(ECHO_LINES)
# The user band folds the prompt after three rows, so these lines are on a row of their
# own only when the tool block shows its output.
HIDDEN_LINES = ECHO_LINES[3:]


def ask_ready(state):
    """The leader label and the model are in the header and the prompt is empty."""
    lines = state['text'].split('\n')
    return (LEADER_LABEL.search(lines[0]) is not None and 'faux-1' in lines[0] and
            any(line.startswith(' ❯ Ask anything') for line in lines))


def ask_ready_in_process(state):
    """The in-process label is in the header: the leader connect failed at startup."""
    lines = state['text'].split('\n')
    return ('in-process' in lines[0] and LEADER_LABEL.search(lines[0]) is None and 'faux-1' in lines[0] and
            any(line.startswith(' ❯ Ask anything') for line in lines))


def screen_lines(state):
    return state['text'].split('\n')


def rule(columns):
    return '─' * columns


def pane_rows(lines, columns):
    """The rows of the block pane: between the header rule and the rule above the prompt slot."""
    top = max((i for i in range(2, len(lines) - 2) if lines[i] == rule(columns)), default=1)
    return lines[2:top]


def assert_shell(state, columns, rows, link='leader'):
    """The parts that every frame of the alternate screen has.

    Row 1 is the header and row 2 its rule. The last row is the shortcut bar, and the row
    above it is the rule under the prompt slot. No row is wider than the terminal.
    """
    lines = screen_lines(state)
    assert len(lines) == rows, f'expected {rows} rows, got {len(lines)}: {state["text"]!r}'
    header = lines[0]
    if link == 'in-process':
        assert HEADER_IN_PROCESS.match(header), f'header differs: {header!r}'
    elif columns < SHORT_WIDTH:
        assert HEADER_SHORT.match(header), f'short header differs: {header!r}'
    else:
        assert HEADER_LONG.match(header), f'header differs: {header!r}'
    assert sum(line.startswith('◢◤ ask') for line in lines) == 1, f'header count differs: {state["text"]!r}'
    assert lines[1] == rule(columns), f'header rule differs: {lines[1]!r}'
    assert lines[rows - 2] == rule(columns), f'prompt rule differs: {lines[rows - 2]!r}'
    assert lines[rows - 1].startswith((BAR_PROMPT, BAR_LIST)), f'shortcut bar differs: {lines[rows - 1]!r}'
    too_wide = [line for line in lines if len(line) > columns]
    assert not too_wide, f'rows wider than {columns} columns: {too_wide[:2]!r}'
    return lines


def assert_idle_screen(state, columns, rows, link='leader'):
    """Header, one empty prompt row between two rules, the placeholder and the cursor in it."""
    lines = assert_shell(state, columns, rows, link)
    assert lines[rows - 1].startswith(BAR_PROMPT), f'idle bar differs: {lines[rows - 1]!r}'
    assert lines[rows - 4] == rule(columns), f'rule above the prompt differs: {lines[rows - 4]!r}'
    assert lines[rows - 3].startswith(' ❯ Ask anything'), f'placeholder differs: {lines[rows - 3]!r}'
    if columns >= SHORT_WIDTH:
        assert lines[rows - 3].startswith(' ❯ Ask anything  ·  / commands  ·  ctrl+l model'), lines[rows - 3]
        assert 'faux-1' in lines[0], f'model is not in the header: {lines[0]!r}'
    assert state['cursor'] == {'x': 3, 'y': rows - 3}, f'cursor={state["cursor"]}'


def assert_slot(lines, title, columns, rows, hint):
    """A selector or a box in the prompt slot: the rule above its title, full-width rows,
    the hint under the bottom border, then the rule and the bar. The block pane stays."""
    tops = [i for i, line in enumerate(lines) if line.startswith(title)]
    assert len(tops) == 1, f'expected one {title.strip()!r}: {lines!r}'
    top = tops[0]
    assert lines[top - 1] == rule(columns), f'no rule above the slot: {lines[top - 1]!r}'
    bottom = next((i for i in range(top, len(lines)) if lines[i].startswith('╰')), None)
    assert bottom is not None, 'the slot has no bottom border'
    for row in range(top, bottom + 1):
        assert len(lines[row]) == columns, f'slot row {row} is {len(lines[row])} columns, expected {columns}'
    assert lines[bottom + 1].strip() == hint, f'hint differs: {lines[bottom + 1]!r}'
    assert bottom + 2 == rows - 2, f'the slot does not end above the bar: bottom={bottom}'
    assert not any(line.startswith(' ❯ Ask anything') for line in lines), 'the slot must replace the prompt'
    return top, bottom


def assert_selector_screen(state, columns, rows):
    lines = assert_shell(state, columns, rows)
    top, bottom = assert_slot(lines, '╭─ Select model ', columns, rows, SELECTOR_HINT)
    # The leader lists usable models grouped by provider: a header row for the
    # provider and the model row under it.
    current = [i for i in range(top, bottom) if lines[i].startswith('│ ❯   faux-1') and re.search(r'current\s+│$', lines[i])]
    assert len(current) == 1, f'current model row differs: {lines!r}'
    assert lines[current[0] - 1].startswith('│   ● faux'), f'provider header differs: {lines!r}'
    assert any(line.strip() == 'model  faux/faux-1' for line in lines[2:top]), 'the block pane is gone'


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
            # The status text is separated from the name by two or more spaces.
            names.append(re.split(r'\s{2,}', match.group(1))[0])
    return names


def assert_provider_box(state, columns, rows, names=PROVIDER_NAMES):
    """The box has the rows in order, the search row last, full-width rows and the hint."""
    lines = assert_shell(state, columns, rows)
    top, bottom = assert_slot(lines, '╭─ Providers ', columns, rows, PROVIDER_HINT)
    assert provider_names(lines) == names, f'provider rows differ: {provider_names(lines)!r}'
    assert '⌕' in lines[bottom - 1], f'the search row is not the last row: {lines[bottom - 1]!r}'


def draft_row(lines, rows, draft):
    """The prompt row holds the draft once, and the draft is nowhere else."""
    expected = f' ❯ {draft}'
    assert lines[rows - 3] == expected, f'prompt row differs: {lines[rows - 3]!r}'
    assert sum(draft in line for line in lines) == 1, 'draft text is drawn more than once'


def assert_one_draft(state, draft, rows):
    """The draft is in the prompt row once and the cursor sits after its last character."""
    draft_row(screen_lines(state), rows, draft)
    expected = {'x': 3 + len(draft), 'y': rows - 3}
    assert state['cursor'] == expected, f'cursor={state["cursor"]}, expected={expected}'


def wait_one_draft(session, draft, rows):
    """Wait for the draft and cursor to settle; a repaint in progress can show a moved cursor."""
    def settled(state):
        try:
            assert_one_draft(state, draft, rows)
            return True
        except AssertionError:
            return False
    try:
        return session.wait(settled)
    except AssertionError:
        assert_one_draft(session.state(), draft, rows)
        raise


def words(text):
    return re.findall(r'\bm\d{3}\b', text)


def assert_complete_stream(text, columns):
    """The user band shows the start of the prompt, then the reply has every word once in order."""
    lines = text.split('\n')
    assert sum(line.startswith('◢◤ ask') for line in lines) == 1, 'header is missing or drawn twice'
    assert sum(line.startswith('  ❯ m000') for line in lines) == 1, 'prompt band is missing or drawn twice'
    expected = [f'm{i:03d}' for i in range(STREAM_MARKERS)]
    found = words(text)
    assert found[-STREAM_MARKERS:] == expected, f'reply words differ: total={len(found)}, last={found[-3:]}'
    shown = found[:-STREAM_MARKERS]
    assert shown == expected[:len(shown)], f'prompt words differ: {shown[:3]}'
    assert any('… +' in line and 'lines · e to expand' in line for line in lines), 'the long prompt is not folded'
    too_wide = [line for line in lines if len(line) > columns]
    assert not too_wide, f'rows wider than {columns} columns: {too_wide[:2]!r}'


def assert_terminal_restored(raw, label):
    """After exit the main screen is back, the cursor is visible, and mouse reporting and
    bracketed paste are off. The last write of each mode decides."""
    last = {}
    for modes, flag in MODE.findall(raw):
        for mode in modes.split(';'):
            last[mode] = flag
    assert last.get('1049') == 'l', f'{label}: the last screen switch does not leave the alternate screen'
    assert last.get('25') == 'h', f'{label}: the cursor is hidden at the end'
    for mode in MOUSE_MODES:
        assert last.get(mode, 'l') == 'l', f'{label}: mouse mode {mode} is still on'
    assert last.get('2004', 'l') == 'l', f'{label}: bracketed paste is still on'


def assert_mouse_enabled(raw):
    modes = {mode for group, flag in MODE.findall(raw) if flag == 'h' for mode in group.split(';')}
    assert modes & {'1002', '1003'} and '1006' in modes, f'mouse reporting was not enabled: {sorted(modes)}'


def assert_exit_line(state):
    """The main screen holds the exit line as its last row and nothing of the TUI."""
    rows = [line.rstrip() for line in screen_lines(state) if line.strip()]
    assert rows, 'the screen is empty after exit'
    assert EXIT_LINE.fullmatch(rows[-1]), f'last row is not the exit line: {rows[-3:]!r}'
    assert not any(line.startswith('◢◤ ask') or line.startswith((BAR_PROMPT, BAR_LIST)) for line in rows), \
        'a frame of the alternate screen is on the main screen'


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


def ask_environment(home, environ=None, extra=None):
    """The process environment of one `ask` case.

    ASK_* settings of the caller are dropped so that a local configuration cannot
    change the run, and so are the variables that look like credentials, so that no
    case can sign in to a real provider. The faux provider is enabled on purpose.
    """
    environ = os.environ if environ is None else environ
    env = {name: value for name, value in environ.items()
           if not name.startswith('ASK_') and not is_credential_name(name)}
    env.update(ASK_HOME=str(home), ASK_FAUX='1', ASK_FAUX_TPS=str(STREAM_TPS), TERM='xterm-256color', LANG='en_US.UTF-8')
    env.update(extra or {})
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
    """A session of the built `ask`, with the screen geometry of its case."""

    link = 'leader'

    @property
    def columns(self):
        return self.size[0]

    @property
    def rows(self):
        return self.size[1]

    def prompt_is_empty(self, state):
        return any(line.startswith(' ❯ Ask anything') for line in screen_lines(state))

    def wait_empty_prompt(self):
        return self.wait(self.prompt_is_empty)

    def settle(self, quiet=0.3, timeout=8):
        """Wait until the screen text has not changed for `quiet` seconds.

        The engine can lose a mouse report that it writes within a few milliseconds
        after the last repaint of a reply. The same report through a plain PTY is never
        lost, so a mouse report is sent only after the screen is quiet.
        """
        deadline = time.monotonic() + timeout
        last, since = None, time.monotonic()
        while time.monotonic() < deadline:
            text = self.state()['text']
            if text != last:
                last, since = text, time.monotonic()
            elif time.monotonic() - since >= quiet:
                return
            time.sleep(0.05)
        raise AssertionError('the screen did not settle')

    def sgr(self, button, column, row, release=False):
        """One SGR mouse report at a 1-based cell. The engine forwards the bytes as they are."""
        self.settle()
        self.write(f'\x1b[<{button};{column};{row}{"m" if release else "M"}')

    def wheel(self, direction, column=None, row=None):
        self.sgr(64 if direction == 'up' else 65, column or self.columns // 2, row or self.rows // 2)

    def click(self, column, row):
        self.sgr(0, column, row)
        self.sgr(0, column, row, release=True)

    def expect_exit(self, label):
        """The program has ended: the terminal is restored and the exit line is the last row."""
        self.call('wait', 'exit', '--timeout', '10000', timeout=15)
        raw = self.recording()
        assert_terminal_restored(raw, label)
        assert_mouse_enabled(raw)
        self.capture('final')
        assert_exit_line(self.state())
        return raw

    def quit(self):
        """ctrl+c clears a draft; ctrl+d on the empty prompt ends the program."""
        if not self.prompt_is_empty(self.state()):
            self.key('ctrl+c')
            self.wait_empty_prompt()
        self.key('ctrl+d')
        self.expect_exit('ctrl+d')


def idle_screen(session):
    # The header is drawn after the first frame and the model name comes with the
    # state answer, so wait until the idle screen is complete.
    def idle(state):
        try:
            assert_idle_screen(state, session.columns, session.rows, session.link)
            return True
        except AssertionError:
            return False
    session.wait(idle)
    state = session.state()
    assert_idle_screen(state, session.columns, session.rows, session.link)
    return state


def ask_idle(session):
    idle_screen(session)
    raw = session.recording()
    assert ALT_ON in raw, 'the alternate screen was not entered'
    assert PURGE not in raw, 'the scrollback was purged'
    assert_mouse_enabled(raw)
    session.capture('idle')
    session.quit()
    return {'screen': 'idle', 'columns': session.columns, 'rows': session.rows}


def send_echo_turn(session):
    """One prompt that calls the echo tool with nine lines, then wait for the usage row."""
    session.write(PASTE_BEGIN + ECHO_PROMPT + PASTE_END)
    session.wait(lambda s: any(line.strip() == ECHO_LINES[-1] for line in screen_lines(s)))
    session.key('enter')
    session.wait(lambda s: USAGE_ROW.search(s['text']) is not None, timeout=40)
    session.wait_empty_prompt()


TOOL_ROW = re.compile(r'^(?:┃ |  )([◆›⌄]) echo out-01')


def tool_rows(state, columns):
    """Rows of the echo tool block, from its first row to the blank row that ends it."""
    lines = pane_rows(screen_lines(state), columns)
    start = next((i for i, line in enumerate(lines) if TOOL_ROW.match(line)), None)
    assert start is not None, f'no tool row: {lines!r}'
    end = start + 1
    while end < len(lines) and lines[end].strip():
        end += 1
    return lines[start:end]


def ask_turn(session):
    """A turn with a tool: one compact row, no output; fold keys; the viewer and back."""
    idle_screen(session)
    columns, rows = session.columns, session.rows
    send_echo_turn(session)
    state = session.state()
    assert_shell(state, columns, rows, session.link)
    pane = pane_rows(screen_lines(state), columns)
    # Collapsed: one step row, the output lines are not on the screen, and the reply is
    # one paragraph with the tool text (the only row that holds it besides the step row).
    assert sum(bool(TOOL_ROW.match(line)) for line in pane) == 1, f'expected one tool row: {pane!r}'
    assert len(tool_rows(state, columns)) == 1, f'the tool block has output rows: {pane!r}'
    assert not any(line.strip() in HIDDEN_LINES for line in pane), f'tool output is visible: {pane!r}'
    assert not any(line.startswith('┃') for line in pane), 'a block is selected before Tab'
    assert sum(line == '  ' + ECHO_REPLY for line in pane) == 1, f'reply row differs: {pane!r}'
    session.capture('collapsed')

    # Tab selects the last block, the reply, which cannot fold. k selects the tool step.
    session.key('tab')
    state = session.wait(lambda s: screen_lines(s)[rows - 1].startswith(BAR_LIST))
    pane = pane_rows(screen_lines(state), columns)
    assert sum(line == '┃ ' + ECHO_REPLY for line in pane) == 1, f'the reply is not selected: {pane!r}'
    session.key('k')
    state = session.wait(lambda s: any(line.startswith('  › echo out-01') for line in screen_lines(s)))
    assert not any(line.startswith('┃') for line in pane_rows(screen_lines(state), columns)), 'a collapsed step has a rail'

    # e: Truncated. Nine lines show as the first three, a hint and the last three.
    session.key('e')
    state = session.wait(lambda s: 'e fold: truncated' in screen_lines(s)[rows - 1])
    block = tool_rows(state, columns)
    assert block[0].startswith('┃ ◆ echo out-01'), block[0]
    assert [line.rstrip() for line in block[1:]] == [
        '┃   out-01', '┃   out-02', '┃   out-03', '┃   ⋮ 3 more lines · e to expand · Enter full screen',
        '┃   out-07', '┃   out-08', '┃   out-09'], f'truncated rows differ: {block!r}'
    assert not any(line.lstrip('┃ ').startswith('$ ') for line in block), 'a command row shows in Truncated'
    session.capture('truncated')

    # e again: Expanded shows every line and the open mark.
    session.key('e')
    state = session.wait(lambda s: 'e fold: expanded' in screen_lines(s)[rows - 1])
    block = tool_rows(state, columns)
    assert block[0].startswith('┃ ⌄ echo out-01'), block[0]
    assert [line.rstrip() for line in block[1:]] == [f'┃   {text}' for text in ECHO_LINES], f'expanded rows differ: {block!r}'

    # e once more: back to one row.
    session.key('e')
    state = session.wait(lambda s: 'e fold: collapsed' in screen_lines(s)[rows - 1])
    assert len(tool_rows(state, columns)) == 1
    assert not any(line.strip() in HIDDEN_LINES for line in pane_rows(screen_lines(state), columns))

    # Enter opens the full-screen viewer; Esc returns to the same selection.
    session.key('enter')
    state = session.wait(lambda s: screen_lines(s)[0].startswith(' ← back  echo out-01'))
    lines = screen_lines(state)
    assert re.search(r'\d+(\.\d+)?m?s$', lines[0].rstrip()), f'viewer title has no time: {lines[0]!r}'
    assert lines[1] == rule(columns), lines[1]
    assert lines[rows - 2] == rule(columns), lines[rows - 2]
    assert lines[rows - 1].startswith(VIEWER_BAR), f'viewer bar differs: {lines[rows - 1]!r}'
    body = [line.rstrip() for line in lines[2:rows - 2]]
    assert [line for line in body if line.strip() in ECHO_LINES] == [f'    {text}' for text in ECHO_LINES], body
    assert not any(line.startswith('◢◤ ask') for line in lines), 'the header shows in the viewer'
    session.capture('viewer')
    session.key('escape')
    state = session.wait(lambda s: screen_lines(s)[0].startswith('◢◤ ask'))
    assert any(line.startswith('  › echo out-01') for line in screen_lines(state)), 'the selection moved'
    assert screen_lines(state)[rows - 1].startswith(BAR_LIST), 'the list lost its focus'

    # Esc in the list returns to the prompt.
    session.key('escape')
    session.wait(lambda s: screen_lines(s)[rows - 1].startswith(BAR_PROMPT))
    assert PURGE not in session.recording(), 'the scrollback was purged'
    session.quit()
    return {'screen': 'tool turn', 'folds': ['collapsed', 'truncated', 'expanded'], 'viewer': True,
            'in_process': session.link == 'in-process'}


def ask_in_process(session):
    session.link = 'in-process'
    return ask_turn(session)


def start_stream(session):
    """Send the long prompt and wait until the reply streams, then type a draft."""
    session.write(STREAM_PROMPT)
    session.key('enter')
    session.wait(lambda s: 'Working…' in s['text'] and
                 any(line.strip().startswith('m000') and '❯' not in line for line in screen_lines(s)), timeout=20)
    session.write('draft')
    wait_one_draft(session, 'draft', session.rows)


def ask_resize(session):
    """Resize 80x24 to 120x40 to 40x14 while a reply streams, with a draft in the prompt."""
    idle_screen(session)
    start_stream(session)
    for columns, rows in [(120, 40), (40, 14)]:
        session.resize(columns, rows)

        def settled(state):
            try:
                assert_shell(state, columns, rows)
                assert_one_draft(state, 'draft', rows)
                return True
            except AssertionError:
                return False
        try:
            state = session.wait(settled)
        except AssertionError:
            state = session.state()
            assert_shell(state, columns, rows)
            assert_one_draft(state, 'draft', rows)
            raise
        assert 'Working…' in state['text'], f'the reply ended before the check at {columns}x{rows}'
        assert sum(line.startswith('  ❯ m000') for line in screen_lines(state)) <= 1
        session.capture(f'resized-{columns}x{rows}')
    session.wait(lambda s: USAGE_ROW.search(s['text']) is not None, timeout=60)
    # A wide terminal shows the whole reply, so the words can be checked in order.
    session.resize(120, 40)
    state = session.wait(lambda s: USAGE_ROW.search(s['text']) is not None)
    assert_shell(state, 120, 40)
    wait_one_draft(session, 'draft', 40)
    assert_complete_stream(session.text_full(), 120)
    raw = session.recording()
    assert PURGE not in raw, 'the scrollback was purged on resize'
    assert_mouse_enabled(raw)
    session.capture('final-frame')
    session.quit()
    return {'resize': '80x24 to 120x40 to 40x14 while streaming', 'draft_and_cursor_kept': True}


def ask_selector(session):
    """The model selector takes the prompt slot, from `/model` and from ctrl+l, and keeps the draft."""
    idle_screen(session)
    columns, rows = session.columns, session.rows
    session.write('/model')
    session.key('enter')
    state = session.wait(lambda s: 'Select model' in s['text'])
    assert_selector_screen(state, columns, rows)
    session.capture('selector-command')
    session.key('escape')
    session.wait(lambda s: 'Select model' not in s['text'])
    idle_screen(session)

    session.write('draft')
    wait_one_draft(session, 'draft', rows)
    session.key('ctrl+l')
    state = session.wait(lambda s: 'Select model' in s['text'])
    assert_selector_screen(state, columns, rows)
    session.capture('selector-ctrl-l')
    session.key('escape')
    session.wait(lambda s: 'Select model' not in s['text'])
    wait_one_draft(session, 'draft', rows)
    session.quit()
    return {'screen': 'selector', 'draft_kept': True}


def ask_providers(session):
    """Open the provider box, filter it, open a key box, paste a fake key and go back."""
    idle_screen(session)
    columns, rows = session.columns, session.rows
    session.write('/providers')
    session.key('enter')
    state = session.wait(lambda s: 'Type to search' in s['text'] and
                         all(name in s['text'] for name in PROVIDER_NAMES))
    assert_provider_box(state, columns, rows)
    session.capture('providers')

    # The filter keeps the rows with the typed text, and enter on a row with no
    # saved credential opens its key box at once.
    session.write('key')
    state = session.wait(lambda s: '⌕ key' in s['text'])
    assert provider_names(screen_lines(state)) == [n for n in PROVIDER_NAMES if 'key' in n], state['text']
    session.key('ctrl+u')
    session.wait(lambda s: 'Type to search' in s['text'])
    session.write('openai api')
    session.wait(lambda s: '⌕ openai api' in s['text'])
    session.key('enter')
    state = session.wait(lambda s: 'Paste your API key.' in s['text'])
    lines = screen_lines(state)
    assert any(line.startswith('│  key  paste or type the key; it stays hidden') for line in lines), state['text']
    assert not any(line.startswith(' ❯ Ask anything') for line in lines), 'the key box must replace the prompt'
    assert lines[0].startswith('◢◤ ask'), 'the header is gone'

    # The pasted key shows as (hidden) and is on no row of the screen.
    session.write(PASTE_BEGIN + ASK_FAKE_KEY + PASTE_END, secret=ASK_FAKE_KEY)
    state = session.wait(lambda s: any(line.startswith('│  key  (hidden)') for line in screen_lines(s)))
    assert ASK_FAKE_KEY not in state['text'] and ASK_FAKE_KEY not in session.text_full(), 'the key is on the screen'
    session.capture('key-box')

    # esc cancels the key box and brings the prompt back; the key is dropped.
    session.key('escape')
    session.wait(lambda s: 'Paste your API key.' not in s['text'] and session.prompt_is_empty(s))
    assert ASK_FAKE_KEY not in session.text_full()
    raw = session.recording()
    assert ASK_FAKE_KEY not in raw, 'the key was written to the terminal'
    session.capture('closed')
    session.quit()
    return {'screen': 'providers', 'rows': len(PROVIDER_NAMES), 'key_box': True, 'secret_on_screen': False}


def marker_row(state):
    """(row index, text) of the bottom marker, or None."""
    found = [(i, line) for i, line in enumerate(screen_lines(state)) if END_MARKER.match(line)]
    assert len(found) <= 1, f'more than one marker: {found!r}'
    return found[0] if found else None


def assert_marker(state, columns, rows, form):
    """The marker is centered on the last row of the block pane."""
    found = marker_row(state)
    assert found is not None, f'no bottom marker: {state["text"]!r}'
    row, line = found
    text = line.strip()
    assert re.fullmatch(form, text), f'marker text differs: {text!r}'
    assert row == rows - 5, f'marker is on row {row}, expected the last pane row {rows - 5}'
    assert line.index('▼') == (columns - len(text)) // 2, f'marker is not centered: {line!r}'


def ask_mouse(session):
    """The wheel scrolls the block pane and shows the bottom marker; a click on it jumps to the end."""
    idle_screen(session)
    columns, rows = session.columns, session.rows
    # Two long turns fill the pane, so that it can scroll.
    for _ in range(2):
        session.write(STREAM_PROMPT)
        session.key('enter')
        session.wait(lambda s: USAGE_ROW.search(s['text']) is not None, timeout=60)
        session.wait_empty_prompt()
        session.wait(lambda s: 'Working…' not in s['text'])
    state = session.state()
    assert marker_row(state) is None, 'a marker shows while the pane follows the end'
    before = screen_lines(state)[2]

    session.wheel('up')
    state = session.wait(lambda s: marker_row(s) is not None)
    assert_marker(state, columns, rows, r'▼ End')
    top = screen_lines(state)[2]
    assert top != before, 'the wheel did not move the pane'
    session.capture('scrolled')

    # New output does not move the window; the marker counts the rows below it.
    session.write('second')
    session.key('enter')
    # The status row takes a pane row while the reply streams, so the marker moves with it.
    state = session.wait(lambda s: 'new lines' in (marker_row(s) or (0, ''))[1] and 'Working…' not in s['text'],
                         timeout=20)
    assert_marker(state, columns, rows, r'▼ \d+ new lines · End')
    assert screen_lines(state)[2] == top, 'new output moved the window'
    session.capture('new-output')

    # A click on the marker jumps to the end and follows again.
    session.click(columns // 2 + 1, rows - 4)
    state = session.wait(lambda s: marker_row(s) is None)
    assert any(line.strip() == 'second' for line in screen_lines(state)), 'the end is not in view'

    # The wheel up and then down to the last row follows again without a click.
    session.wheel('up')
    session.wait(lambda s: marker_row(s) is not None)
    for _ in range(3):
        session.wheel('down')
    session.wait(lambda s: marker_row(s) is None)
    assert PURGE not in session.recording()
    session.quit()
    return {'screen': 'scroll', 'wheel': 'SGR', 'marker': 'centered on the last pane row'}


def reply_text(session, prompt, reply):
    """Send one prompt and wait until the reply (`reply` is the text of the faux answer) is done."""
    session.write(prompt)
    session.key('enter')
    session.wait(lambda s: any(line.strip() == reply for line in screen_lines(s)) and
                 USAGE_ROW.search(s['text']) is not None, timeout=40)
    session.wait_empty_prompt()


def ask_copy(session):
    """/copy sends the last reply text to the terminal clipboard (OSC 52) and nothing else."""
    idle_screen(session)
    # Before any reply there is nothing to copy, and nothing is sent to the terminal.
    session.write('/copy')
    session.key('enter')
    session.wait(lambda s: 'Nothing to copy' in s['text'])
    assert OSC52 not in session.recording(), 'a copy with no reply wrote OSC 52'
    session.capture('nothing-to-copy')

    # One tool turn, then a plain reply: the tool output must not be in the copy.
    reply_text(session, 'echo copy-tool-marker', 'copy-tool-marker')
    reply = 'plain reply for copy words'
    reply_text(session, reply, reply)
    session.write('/copy')
    session.key('enter')
    session.wait(lambda s: 'Sent to the terminal clipboard' in s['text'])
    raw = session.recording()
    assert raw.count(OSC52) == 1, f'expected one OSC 52 sequence, found {raw.count(OSC52)}'
    matches = OSC52_SEQUENCE.findall(raw)
    assert len(matches) == 1, 'the OSC 52 sequence has no terminator'
    decoded = base64.b64decode(matches[0], validate=True).decode()
    assert decoded == reply, f'clipboard text differs: {decoded!r}'
    assert 'copy-tool-marker' not in decoded
    session.capture('copied')
    session.quit()
    return {'screen': 'copy', 'osc52_sequences': 1, 'payload': 'last reply text only'}


def open_viewer(session):
    """A tool turn, then the viewer of the tool block, so that an exit path must leave a full-screen frame."""
    idle_screen(session)
    send_echo_turn(session)
    session.key('tab')
    session.wait(lambda s: screen_lines(s)[session.rows - 1].startswith(BAR_LIST))
    session.key('k')
    session.wait(lambda s: any(line.startswith('  › echo out-01') for line in screen_lines(s)))
    session.key('enter')
    session.wait(lambda s: screen_lines(s)[0].startswith(' ← back  echo out-01'))


def ask_exit_quit(session):
    """`/quit` leaves the alternate screen and prints the exit line."""
    idle_screen(session)
    send_echo_turn(session)
    session.write('/quit')
    session.key('enter')
    session.expect_exit('/quit')
    return {'exit': '/quit', 'terminal_restored': True, 'exit_line': True}


def ask_exit_ctrl_c(session):
    """ctrl+c on the empty prompt asks, and the second one ends the program."""
    idle_screen(session)
    send_echo_turn(session)
    session.key('ctrl+c')
    session.wait(lambda s: 'Press ctrl+c again to quit' in s['text'])
    session.key('ctrl+c')
    session.expect_exit('ctrl+c')
    return {'exit': 'ctrl+c twice', 'terminal_restored': True, 'exit_line': True}


def ask_exit_signal(session, sig, label):
    """A signal while the viewer is open restores the terminal and prints the exit line."""
    open_viewer(session)
    # The TUI is the one process whose whole command line is the binary of this run.
    pids = [pid for pid, command in process_table() if command.strip() == session.target]
    assert len(pids) == 1, f'expected one ask process, found {pids}'
    os.kill(pids[0], sig)
    session.expect_exit(label)
    return {'exit': label, 'terminal_restored': True, 'exit_line': True}


def ask_exit_sigterm(session):
    return ask_exit_signal(session, signal.SIGTERM, 'SIGTERM')


def ask_exit_sighup(session):
    return ask_exit_signal(session, signal.SIGHUP, 'SIGHUP')


CASES = {
    'ask-idle': (ask_idle, ASK_COLUMNS, ASK_ROWS, {}),
    'ask-turn': (ask_turn, ASK_COLUMNS, ASK_ROWS, {}),
    # A slower reply, so that it outlives the three sizes.
    'ask-resize': (ask_resize, ASK_COLUMNS, ASK_ROWS, {'ASK_FAUX_TPS': '10'}),
    'ask-selector': (ask_selector, ASK_COLUMNS, ASK_ROWS, {}),
    'ask-providers': (ask_providers, ASK_COLUMNS, ASK_ROWS, {}),
    'ask-mouse': (ask_mouse, ASK_COLUMNS, ASK_ROWS, {'ASK_FAUX_TPS': '200'}),
    'ask-exit-quit': (ask_exit_quit, ASK_COLUMNS, ASK_ROWS, {}),
    'ask-exit-ctrl-c': (ask_exit_ctrl_c, ASK_COLUMNS, ASK_ROWS, {}),
    'ask-exit-sigterm': (ask_exit_sigterm, ASK_COLUMNS, ASK_ROWS, {}),
    'ask-exit-sighup': (ask_exit_sighup, ASK_COLUMNS, ASK_ROWS, {}),
    'ask-copy': (ask_copy, ASK_COLUMNS, ASK_ROWS, REMOTE_ENV),
    'ask-in-process': (ask_in_process, ASK_COLUMNS, ASK_ROWS, {}),
}
assert list(CASES) == ASK_CASES


def compile_ask(base, artifacts):
    binary = base / 'ask'
    with (artifacts / 'go-compile.txt').open('w') as output:
        result = subprocess.run(['go', 'build', '-o', str(binary), './cmd/tui'], cwd=ROOT,
                                env=dict(os.environ, GOWORK='off'), stdout=output, stderr=subprocess.STDOUT, timeout=600)
    assert result.returncode == 0, 'go build of ./cmd/tui failed; see go-compile.txt'
    return binary


def run_ask(args, cli, run_id, artifacts, results):
    """Run the cases; each case has its own ASK_HOME and its own leader."""
    selected = [name for name in ASK_CASES if not args.case or name in args.case]
    # A Unix socket path is short-limited, so the base is under /tmp and not the user temp dir.
    base = Path(tempfile.mkdtemp(prefix='askl-', dir='/tmp')).resolve()
    secrets = secret_values(os.environ)
    environment = {'set_names': list(ASK_ENV_NAMES), 'dropped_prefix': 'ASK_',
                   'dropped_credential_names': sorted(name for name in os.environ if is_credential_name(name)),
                   'secret_variable_count': len(secrets)}
    (artifacts / 'ask-environment.json').write_text(json.dumps(environment, indent=2) + '\n')
    try:
        binary = compile_ask(base, artifacts)
        for backend in args.backend or ['alacritty', 'ghostty']:
            for scenario in selected:
                test, cols, rows, extra = CASES[scenario]
                name = f'askcore-e2e-{run_id}-ask-{backend}-{scenario}'
                record = {'backend': backend, 'scenario': scenario, 'session': name, 'result': 'fail'}
                home = base / f'{backend}-{scenario}'
                work = base / f'work-{backend}-{scenario}'
                work.mkdir()
                in_process = scenario in IN_PROCESS_CASES
                if in_process:
                    # The real home is behind a symlink; the leader connect refuses it.
                    real_home = base / f'real-{backend}-{scenario}'
                    real_home.mkdir(mode=0o700)
                    home.symlink_to(real_home)
                env = ask_environment(home, extra=extra)
                env_names = ASK_ENV_NAMES + tuple(sorted(set(extra) - set(ASK_ENV_NAMES)))
                case_dir = artifacts / backend / scenario
                session = None
                try:
                    session = AskSession(cli, name, backend, binary, work, case_dir, cols=cols, rows=rows, env=env,
                                         env_names=env_names,
                                         ready=ask_ready_in_process if in_process else ask_ready, ready_timeout=30)
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
                        if in_process:
                            assert not leader_processes(binary), f'a leader runs in an in-process case: {leader_processes(binary)}'
                        else:
                            stop_leader(binary, home, env, case_dir)
                    except Exception as error:
                        record.update(result='fail', leader_cleanup_error=str(error))
                    results['cases'].append(record)
                    (artifacts / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
                print(f'{backend}/{scenario}: {record["result"]}', flush=True)
        left = leader_processes(binary)
        assert not left, f'leader processes left after the run: {left}'
    finally:
        shutil.rmtree(base, ignore_errors=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend', action='append', choices=['alacritty', 'ghostty', 'rio'],
                        help='Terminal engine; default: alacritty and ghostty')
    parser.add_argument('--artifacts', type=Path, help='Evidence directory; default: e2e/tui/artifacts/<run-id>')
    parser.add_argument('--case', action='append', choices=ASK_CASES, help='Select cases; default: all')
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
    product_before = product_hashes()
    results = {'cli_version': version, 'cases': []}
    try:
        run_ask(args, cli, run_id, artifacts, results)
    except Exception as error:
        results['error'] = str(error)
    product_after = product_hashes()
    (artifacts / 'boundaries.json').write_text(json.dumps(
        {'product_before': product_before, 'product_after': product_after}, indent=2) + '\n')
    if product_after != product_before:
        changed = sorted(path for path in {*product_before, *product_after}
                         if product_before.get(path) != product_after.get(path))
        results['boundary_error'] = f'Product files changed during the run: {changed[:5]}'
    (artifacts / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
    # Evidence is kept only when no credential value from the inherited environment is in it.
    leaked = scan_secrets(artifacts, {**secret_values(os.environ), 'ASK_FAKE_KEY': ASK_FAKE_KEY})
    if leaked:
        results = {'secret_error': 'credential values found in artifacts; the files were removed',
                   'files': sorted(leaked), 'variables': sorted({n for names in leaked.values() for n in names}),
                   'cases': [{'scenario': r['scenario'], 'result': 'fail'} for r in results['cases']]}
        (artifacts / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
        print(f'Credential values found in {len(leaked)} artifact file(s); removed. Variables: {results["variables"]}', flush=True)
    if results.get('error'):
        print(f'Error: {results["error"]}', flush=True)
    if results.get('boundary_error'):
        print(f'Error: {results["boundary_error"]}', flush=True)
    print(f'Artifacts: {artifacts}', flush=True)
    ok = (not leaked and results['cases'] and not results.get('error') and not results.get('boundary_error') and
          all(r['result'] == 'pass' for r in results['cases']))
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main())
