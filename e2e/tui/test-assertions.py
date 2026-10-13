"""Independent hand-written controls for the terminal assertions of run.py."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from subprocess import CompletedProcess

from run import (ASK_FAKE_KEY, ECHO_LINES, ECHO_REPLY, PROVIDER_HINT, PROVIDER_NAMES, SELECTOR_HINT, Session,
                 assert_complete_stream, assert_exit_line, assert_idle_screen, assert_marker, assert_mouse_enabled,
                 assert_one_draft, assert_provider_box, assert_selector_screen, assert_shell, assert_terminal_restored,
                 ask_environment, ask_ready, ask_ready_in_process, hide_typed_secret, marker_row, output_bytes,
                 pane_rows, provider_names, scan_secrets, secret_values, tool_rows)

COLUMNS, ROWS = 80, 24
RULE = '─' * COLUMNS
HEADER = '◢◤ ask  ● leader 7c1e  ⌂ ~/src/p         faux-1  ▱▱▱▱▱▱▱▱ 0%  $0.00'
BAR = ' Tab blocks · ctrl+t thinking · ctrl+l model · / commands'
PLACEHOLDER = ' ❯ Ask anything  ·  / commands  ·  ctrl+l model'


def frame(pane=(), prompt=PLACEHOLDER, bar=BAR, header=HEADER, rows=ROWS, columns=COLUMNS):
    """A full frame: header, rule, pane rows, rule, one prompt row, rule, bar."""
    body = list(pane) + [''] * (rows - 6 - len(pane))
    assert len(body) == rows - 6, 'the pane of the control frame is too tall'
    return '\n'.join([header, '─' * columns, *body, '─' * columns, prompt, '─' * columns, bar])


def slot_frame(title, rows_text, hint, pane=('  model  faux/faux-1',), columns=COLUMNS):
    """A frame whose prompt slot holds a box with `rows_text` rows."""
    inner = [('│ ' + text).ljust(columns - 1) + '│' for text in rows_text]
    box = [('╭─ ' + title + ' ').ljust(columns - 1, '─') + '╮', *inner, '╰' + '─' * (columns - 2) + '╯', '   ' + hint]
    body_rows = ROWS - 2 - 1 - 1 - len(box) - 1
    body = list(pane) + [''] * (body_rows - len(pane))
    return '\n'.join([HEADER, RULE, *body, RULE, *box, RULE, BAR])


class ShellControls(unittest.TestCase):
    def test_shell_accepts_the_idle_frame(self):
        state = {'text': frame(), 'cursor': {'x': 3, 'y': ROWS - 3}}
        assert_shell(state, COLUMNS, ROWS)
        assert_idle_screen(state, COLUMNS, ROWS)

    def test_shell_rejects_a_broken_frame(self):
        good = frame()
        for name, text in [
                ('header gone', good.replace('◢◤ ask', 'ask', 1)),
                ('header twice', good.replace('\n\n', '\n' + HEADER + '\n', 1)),
                ('inline leader label', good.replace('leader 7c1e', 'in-process')),
                ('header rule short', good.replace(RULE, '─' * (COLUMNS - 1), 1)),
                ('bar gone', frame(bar=' something else')),
                ('row too wide', frame(pane=['x' * (COLUMNS + 1)])),
                ('one row short', '\n'.join(good.split('\n')[1:]))]:
            with self.subTest(name), self.assertRaises(AssertionError):
                assert_shell({'text': text, 'cursor': {'x': 3, 'y': ROWS - 3}}, COLUMNS, ROWS)

    def test_short_header_at_40_columns(self):
        short = frame(header='◢◤ ask ● 7c1e            faux-1 0% ', columns=40, rows=14,
                      prompt=' ❯ Ask anything', bar=' Tab blocks · / commands')
        assert_shell({'text': short}, 40, 14)
        with self.assertRaises(AssertionError):
            assert_shell({'text': short.replace('● 7c1e', 'leader')}, 40, 14)

    def test_in_process_header(self):
        header = '◢◤ ask  ○ in-process  ⌂ ~/src/p         faux-1  ▱▱▱▱▱▱▱▱ 0%  $0.00'
        assert_shell({'text': frame(header=header)}, COLUMNS, ROWS, 'in-process')
        with self.assertRaises(AssertionError):
            assert_shell({'text': frame(header=header)}, COLUMNS, ROWS, 'leader')

    def test_idle_cursor_and_placeholder(self):
        state = {'text': frame(), 'cursor': {'x': 3, 'y': ROWS - 3}}
        for invalid in [dict(state, cursor={'x': 4, 'y': ROWS - 3}), dict(state, cursor={'x': 3, 'y': 0}),
                        dict(state, text=frame(prompt=' ❯ typed text')),
                        dict(state, text=frame(header=HEADER.replace('faux-1', 'other')))]:
            with self.subTest(text=invalid['text'][:40], cursor=invalid['cursor']), self.assertRaises(AssertionError):
                assert_idle_screen(invalid, COLUMNS, ROWS)

    def test_ready(self):
        assert ask_ready({'text': frame()})
        assert not ask_ready({'text': frame(header='◢◤ ask  ◌ leader starting')})
        assert not ask_ready({'text': frame(header=HEADER.replace('leader 7c1e', 'in-process'))})
        assert not ask_ready({'text': frame(prompt=' ❯ draft')})
        assert ask_ready_in_process({'text': frame(header=HEADER.replace('● leader 7c1e', '○ in-process'))})
        assert not ask_ready_in_process({'text': frame()})

    def test_one_draft(self):
        state = {'text': frame(prompt=' ❯ draft'), 'cursor': {'x': 8, 'y': ROWS - 3}}
        assert_one_draft(state, 'draft', ROWS)
        for invalid in [dict(state, cursor={'x': 7, 'y': ROWS - 3}), dict(state, cursor={'x': 8, 'y': 0}),
                        dict(state, text=frame(pane=['draft'], prompt=' ❯ draft')), dict(state, text=frame(prompt=' ❯ draf'))]:
            with self.subTest(state=invalid), self.assertRaises(AssertionError):
                assert_one_draft(invalid, 'draft', ROWS)


class SlotControls(unittest.TestCase):
    def selector(self, hint=SELECTOR_HINT, current='current', title='Select model'):
        rows = ['⌕ type to filter', '  ● faux', '❯   faux-1' + ' ' * 40 + '128k  ' + current]
        return slot_frame(title, rows, hint)

    def test_selector_screen(self):
        text = self.selector()
        assert_selector_screen({'text': text}, COLUMNS, ROWS)
        for name, invalid in [('hint', self.selector(hint='gone')), ('current', self.selector(current='other')),
                              ('title', self.selector(title='Pick')),
                              ('draft row', text.replace('⌕ type to filter', '⌕ type to filter').replace(
                                  BAR, BAR + '\n ❯ Ask anything')),
                              ('no rule above', text.replace(RULE + '\n╭', '\n╭'))]:
            with self.subTest(name), self.assertRaises(AssertionError):
                assert_selector_screen({'text': invalid}, COLUMNS, ROWS)
        with self.assertRaises(AssertionError):
            assert_selector_screen({'text': text}, COLUMNS + 1, ROWS)

    def test_selector_keeps_the_block_pane(self):
        gone = slot_frame('Select model', ['⌕ type to filter', '  ● faux', '❯   faux-1' + ' ' * 40 + '128k  current'],
                          SELECTOR_HINT, pane=())
        with self.assertRaises(AssertionError):
            assert_selector_screen({'text': gone}, COLUMNS, ROWS)

    def test_provider_box(self):
        status = {'Alibaba Token Plan API key': 'api key', 'ChatGPT Plus/Pro sign-in': 'signed in'}
        rows = []
        for name in PROVIDER_NAMES:
            marker = '❯' if name == PROVIDER_NAMES[0] else ' '
            suffix = status.get(name, '')
            text = marker + ' ● ' + name
            rows.append(text.ljust(COLUMNS - 4 - len(suffix)) + suffix if suffix else text)
        rows.append(' ⌕ Type to search')
        text = slot_frame('Providers', rows, PROVIDER_HINT)
        assert provider_names(text.split('\n')) == PROVIDER_NAMES
        assert_provider_box({'text': text}, COLUMNS, ROWS)
        for invalid in [text.replace(PROVIDER_HINT, 'enter sign in'), text.replace('OpenAI API key', 'OpenAI key'),
                        text.replace('⌕ Type to search', 'Type to search'), text.replace('╮', ''),
                        text.replace(BAR, BAR + '\n ❯ Ask anything')]:
            with self.subTest(text=invalid), self.assertRaises(AssertionError):
                assert_provider_box({'text': invalid}, COLUMNS, ROWS)


class PaneControls(unittest.TestCase):
    def collapsed(self):
        return ['  model  faux/faux-1', '', '  ❯ echo out-01', '    out-02', '    out-03', '    … +6 lines · e to expand', '',
                '  ◆ echo ' + ECHO_REPLY, '', '  ' + ECHO_REPLY, '  faux-1 · 0.8s · ↑45 ↓16']

    def test_pane_rows_end_at_the_rule_above_the_prompt(self):
        pane = self.collapsed()
        rows = pane_rows(frame(pane=pane).split('\n'), COLUMNS)
        assert [row.rstrip() for row in rows[:len(pane)]] == pane

    def test_tool_rows(self):
        state = {'text': frame(pane=self.collapsed())}
        assert tool_rows(state, COLUMNS) == ['  ◆ echo ' + ECHO_REPLY]
        shown = self.collapsed()
        shown[7:8] = ['┃ ◆ echo ' + ECHO_REPLY, *('┃   ' + line for line in ECHO_LINES[:3])]
        assert len(tool_rows({'text': frame(pane=shown)}, COLUMNS)) == 4
        with self.assertRaises(AssertionError):
            tool_rows({'text': frame(pane=['  nothing'])}, COLUMNS)

    def test_marker(self):
        pane = ['row'] * 17 + ['                                     ▼ End']
        state = {'text': frame(pane=pane)}
        assert marker_row(state) == (ROWS - 5, pane[-1])
        assert_marker(state, COLUMNS, ROWS, r'▼ End')
        for name, invalid in [('off centre', frame(pane=['row'] * 17 + ['                                       ▼ End'])),
                              ('wrong row', frame(pane=['row'] * 16 + [pane[-1], 'row'])),
                              ('wrong text', frame(pane=['row'] * 17 + ['                              ▼ 3 new lines · End'])),
                              ('none', frame(pane=['row']))]:
            with self.subTest(name), self.assertRaises(AssertionError):
                assert_marker({'text': invalid}, COLUMNS, ROWS, r'▼ End')
        new = '                              ▼ 3 new lines · End'
        assert_marker({'text': frame(pane=['row'] * 17 + [new])}, COLUMNS, ROWS, r'▼ \d+ new lines · End')

    def test_complete_stream(self):
        words = [f'm{i:03d}' for i in range(100)]
        reply = '\n'.join('  ' + ' '.join(words[i:i + 20]) for i in range(0, 100, 20))
        text = '◢◤ ask  ● leader 7c1e\n  ❯ ' + ' '.join(words[:20]) + '\n    … +4 lines · e to expand\n\n' + reply
        assert_complete_stream(text, 120)
        for name, invalid in [('missing word', text.replace('m050', 'm049', 1)), ('twice', text + '\n' + reply),
                              ('no fold', text.replace('… +4 lines · e to expand', '')),
                              ('two headers', text + '\n◢◤ ask'), ('wide', text + '\n' + 'x' * 121)]:
            with self.subTest(name), self.assertRaises(AssertionError):
                assert_complete_stream(invalid, 120)


class ExitControls(unittest.TestCase):
    GOOD = ('\x1b[?1049h\x1b[?2004h\x1b[?1003h\x1b[?1006h\x1b[?25l frame \x1b[?25h'
            '\x1b[?1049l\x1b[?25l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?25h\x1b[?2004l bye')

    def test_terminal_restored(self):
        assert_terminal_restored(self.GOOD, 'control')
        assert_mouse_enabled(self.GOOD)
        for name, invalid in [('alt screen left on', self.GOOD.replace('\x1b[?1049l', '')),
                              ('cursor hidden', self.GOOD + '\x1b[?25l'),
                              ('mouse on', self.GOOD.replace('\x1b[?1003l', '')),
                              ('sgr mouse on', self.GOOD.replace('\x1b[?1006l', '')),
                              ('paste on', self.GOOD.replace('\x1b[?2004l', '')),
                              ('alt screen again', self.GOOD + '\x1b[?1049h')]:
            with self.subTest(name), self.assertRaises(AssertionError):
                assert_terminal_restored(invalid, 'control')

    def test_combined_mode_sequence(self):
        assert_terminal_restored('\x1b[?1049;1003;1006h x \x1b[?1049;1003;1006l\x1b[?25h', 'control')
        with self.assertRaises(AssertionError):
            assert_terminal_restored('\x1b[?1049;1003;1006h x \x1b[?1049l\x1b[?25h', 'control')

    def test_mouse_never_enabled(self):
        with self.assertRaises(AssertionError):
            assert_mouse_enabled('\x1b[?1049h\x1b[?1049l')

    def test_exit_line(self):
        assert_exit_line({'text': '  ⏏ Detached from session ac86.\nask: session sess_ac86ab6cdf9e90bc\n\n'})
        for name, invalid in [('missing', '  ⏏ Detached from session ac86.'),
                              ('not last', 'ask: session sess_ac86\nafter'),
                              ('frame left', HEADER + '\nask: session sess_ac86'),
                              ('bar left', BAR + '\nask: session sess_ac86'),
                              ('empty', '\n\n')]:
            with self.subTest(name), self.assertRaises(AssertionError):
                assert_exit_line({'text': invalid})


class SecretControls(unittest.TestCase):
    def test_output_chunk_join(self):
        events = [{'version': 2}, [0.1, 'o', 'abc'], [0.2, 'i', 'typed'], [0.3, 'o', 'def']]
        assert output_bytes('\n'.join(json.dumps(event) for event in events)) == 'abcdef'
        with self.assertRaises(AssertionError):
            output_bytes(json.dumps({'version': 1}))

    def test_typed_secret_is_hidden_in_input_events_only(self):
        header = json.dumps({'version': 2})
        typed = json.dumps([0.1, 'i', '\x1b[200~sk-control-key\x1b[201~'])
        shown = json.dumps([0.2, 'o', 'echoed sk-control-key'])
        result = hide_typed_secret('\n'.join([header, typed, shown]) + '\n', 'sk-control-key')
        assert '<secret>' in result
        # An output event keeps the value, so a product leak still shows in the scan.
        assert result.count('sk-control-key') == 1 and 'echoed sk-control-key' in result

    def test_recording_keeps_typed_secret_out_of_call_artifacts(self):
        recording = '\n'.join([json.dumps({'version': 2}), json.dumps([0.1, 'i', ASK_FAKE_KEY])]) + '\n'
        with tempfile.TemporaryDirectory() as temporary:
            session = Session.__new__(Session)
            session.cli, session.name = 'control-cli', 'control-session'
            session.artifacts, session.calls = Path(temporary), []
            with patch('run.subprocess.run', return_value=CompletedProcess([], 0, recording, '')):
                assert session.recording() == ''
            assert scan_secrets(temporary, {'fake-key': ASK_FAKE_KEY}) == {}
            leaked = recording + json.dumps([0.2, 'o', ASK_FAKE_KEY]) + '\n'
            with patch('run.subprocess.run', return_value=CompletedProcess([], 0, leaked, '')):
                assert session.recording() == ASK_FAKE_KEY
            assert set(scan_secrets(temporary, {'fake-key': ASK_FAKE_KEY})) == {'commands.json', 'output.cast'}

    def test_secret_scan(self):
        # The values are made up for this control; no real credential is read.
        environ = {'EXAMPLE_API_KEY': 'sk-control-value-1', 'EXAMPLE_TOKEN': 'short', 'EXAMPLE_TOKEN_URL': 'https://example.test/x',
                   'SSH_AUTH_SOCK': '/tmp/agent.sock-long', 'PATH': '/usr/bin:/bin'}
        secrets = secret_values(environ)
        assert secrets == {'EXAMPLE_API_KEY': 'sk-control-value-1'}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'sub').mkdir()
            (root / 'clean.json').write_text('{"environment_names": ["EXAMPLE_API_KEY"]}')
            (root / 'sub' / 'leak.cast').write_text('x sk-control-value-1 y')
            assert scan_secrets(root, secrets) == {'sub/leak.cast': ['EXAMPLE_API_KEY']}
            assert not (root / 'sub' / 'leak.cast').exists() and (root / 'clean.json').exists()
            assert scan_secrets(root, secrets) == {}

    def test_ask_environment_enables_faux_and_drops_credentials(self):
        # Made-up values: the control reads no real credential.
        environ = {'OPENAI_API_KEY': 'sk-control-value-1', 'ANTHROPIC_AUTH_TOKEN': 'control-token-2',
                   'XAI_API_KEY': 's', 'ASK_HOME': '/elsewhere', 'ASK_FAUX': '0', 'SSH_AUTH_SOCK': '/tmp/agent.sock',
                   'PATH': '/usr/bin:/bin', 'HOME': '/home/someone'}
        env = ask_environment('/tmp/home', environ)
        assert env['ASK_FAUX'] == '1' and env['ASK_HOME'] == '/tmp/home'
        for name in ('OPENAI_API_KEY', 'ANTHROPIC_AUTH_TOKEN', 'XAI_API_KEY'):
            assert name not in env, name
        assert env['PATH'] == '/usr/bin:/bin' and env['SSH_AUTH_SOCK'] == '/tmp/agent.sock'
        assert ask_environment('/tmp/home', environ, {'ASK_FAUX_TPS': '10'})['ASK_FAUX_TPS'] == '10'


if __name__ == '__main__':
    unittest.main()
