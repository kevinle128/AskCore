"""Independent hand-written controls for terminal assertions."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from subprocess import CompletedProcess

from run import (ASK_FAKE_KEY, PROVIDER_HINT, PROVIDER_NAMES, PURGE, Session, assert_editor, assert_cursor, assert_frame, assert_idle_screen,
                 assert_no_alt_screen, assert_no_replay, assert_one_draft, assert_provider_box, assert_rebuild_once,
                 assert_resize_replay, assert_selector_screen, assert_stream_text, assert_transcript, ask_environment,
                 ask_ready, hide_typed_secret, output_bytes, provider_names, scan_secrets, secret_values)


class AssertionControls(unittest.TestCase):
    def test_resize_generations(self):
        history = 'T0 startup\nG1[0000]\nG1[0001]'
        repair = '\x1b[2J\x1b[3J\x1b[H'
        assert_resize_replay(history + repair + history, 2)
        for invalid in [history, history + repair + 'G1[0000]',
                        history + repair + history + 'G1[0001]',
                        history + repair + 'G1[0001]G1[0000]',
                        history + repair + 'G1[0000]G1[0001]',
                        history + '\x1b[3J' + history,
                        history + repair + history + '\x1b[?1049h']:
            with self.subTest(raw=invalid), self.assertRaises(AssertionError):
                assert_resize_replay(invalid, 2)

    def test_exact_transcript(self):
        assert_transcript('G1[0000]\nG1[0001]', 2)
        for text in ['G1[0000]', 'G1[0000]G1[0000]G1[0001]', 'G1[0001]G1[0000]']:
            with self.subTest(text=text), self.assertRaises(AssertionError):
                assert_transcript(text, 2)

    def test_cursor(self):
        state = {'text': 'T0 startup\neditor> draft', 'cursor': {'x': 13, 'y': 1}}
        assert_cursor(state, 13, 'editor> draft')
        for cursor in [{'x': 12, 'y': 1}, {'x': 13, 'y': 0}]:
            with self.subTest(cursor=cursor), self.assertRaises(AssertionError):
                assert_cursor(dict(state, cursor=cursor), 13, 'editor> draft')
        with self.assertRaises(AssertionError):
            assert_cursor(dict(state, text='editor> draft\neditor> draft'), 13, 'editor> draft')

    def test_single_editor(self):
        assert_editor({'text': 'editor> abc界\n😀Z'}, 'editor> abc界')
        for text in ['editor> abc界\neditor> abc界\n😀Z', '界editor> ab\neditor> abc\n界😀Z']:
            with self.subTest(text=text), self.assertRaises(AssertionError):
                assert_editor({'text': text}, 'editor> abc界' if 'abc界' in text else 'editor> abc')

    def test_output_chunk_join(self):
        events = [{'version': 2}, [0.1, 'o', 'G1[00'], [0.2, 'i', 'G1[9999]'], [0.3, 'o', '00]\nG1[0001]']]
        cast = '\n'.join(json.dumps(event) for event in events)
        raw = output_bytes(cast)
        assert_no_replay(raw, 2)
        with self.assertRaises(AssertionError):
            assert_no_replay(raw + 'G1[0001]', 2)
        for erase in ['\x1b[2J', '\x1b[3J', '\x1b[?47h', '\x1b[?1047h', '\x1b[?1049h']:
            with self.subTest(erase=erase), self.assertRaises(AssertionError):
                assert_no_replay(raw + erase, 2)


WIDTH = 60


def box_row(content, width=WIDTH):
    return ('│ ' + content).ljust(width - 1) + '│'


def screen(editor='❯ Ask anything  ·  / commands  ·  ctrl+l model', footer='  ● leader 1cb8   ⌂ ~   faux-1', width=WIDTH):
    rows = ['◢◤ ask   v1 · leader 1cb8', '', '╭' + '─' * (width - 2) + '╮', box_row(editor, width),
            '╰' + '─' * (width - 2) + '╯', footer]
    return '\n'.join(rows)


def selector(hint='enter select  ·  esc close  ·  your draft is kept', current='current', title='Select model'):
    rows = ['◢◤ ask', '', ('╭─ ' + title + ' ').ljust(WIDTH - 1, '─') + '╮', box_row('⌕ type to filter'),
            box_row('  ● faux'), box_row('❯   faux-1    128k  ' + current), '╰' + '─' * (WIDTH - 2) + '╯', '   ' + hint,
            '  ● leader 1cb8   faux-1']
    return '\n'.join(rows)


class AskControls(unittest.TestCase):
    def test_ready_needs_leader_footer(self):
        assert ask_ready({'text': screen()})
        for text in [screen(footer='  ○ in-process   ⌂ ~   faux-1'), screen(footer='  ◌ leader starting'),
                     screen(editor='starting')]:
            with self.subTest(text=text):
                assert not ask_ready({'text': text})

    def test_idle_screen(self):
        state = {'text': screen(), 'cursor': {'x': 4, 'y': 3}}
        assert_idle_screen(state, WIDTH)
        for invalid in [dict(state, cursor={'x': 5, 'y': 3}), dict(state, text=state['text'] + '\n' + state['text']),
                        dict(state, text=state['text'].replace('╮', '')), dict(state, text=screen(footer='  ○ in-process faux-1')),
                        dict(state, text=screen(editor='typed text'))]:
            with self.subTest(text=invalid['text']), self.assertRaises(AssertionError):
                assert_idle_screen(invalid, WIDTH)
        with self.assertRaises(AssertionError):
            assert_idle_screen(state, WIDTH + 1)

    def test_selector_screen(self):
        text = selector()
        assert_selector_screen({'text': text}, WIDTH)
        for invalid in [selector(hint='gone'), selector(current='other'), selector(title='Pick'),
                        text + '\n│ ❯ draft']:
            with self.subTest(text=invalid), self.assertRaises(AssertionError):
                assert_selector_screen({'text': invalid}, WIDTH)
        with self.assertRaises(AssertionError):
            assert_selector_screen({'text': text}, WIDTH + 1)

    def test_provider_box(self):
        status = {'Alibaba Token Plan API key': 'api key', 'ChatGPT Plus/Pro sign-in': 'signed in'}
        rows = ['◢◤ ask', '', ('╭─ Providers ').ljust(WIDTH - 1, '─') + '╮']
        for name in PROVIDER_NAMES:
            suffix = status.get(name, '')
            rows.append(('│ ' + ('❯' if name == PROVIDER_NAMES[0] else ' ') + ' ● ' + name).ljust(WIDTH - 2 - len(suffix) - 1) + ' ' + suffix + ' │'
                        if suffix else box_row(('❯' if name == PROVIDER_NAMES[0] else ' ') + ' ● ' + name))
        rows += [box_row(' ⌕ Type to search'), '╰' + '─' * (WIDTH - 2) + '╯', '   ' + PROVIDER_HINT, '  ● leader 1cb8   faux-1']
        text = '\n'.join(rows)
        assert provider_names(text.split('\n')) == PROVIDER_NAMES
        assert_provider_box({'text': text}, WIDTH)
        for invalid in [text.replace(PROVIDER_HINT, 'enter sign in'), text.replace('OpenAI API key', 'OpenAI key'),
                        text.replace('│ ❯ ● Alibaba', '│ ❯ Ask anything\n│ ❯ ● Alibaba'),
                        text.replace('⌕ Type to search', 'Type to search'), text.replace('╮', '')]:
            with self.subTest(text=invalid), self.assertRaises(AssertionError):
                assert_provider_box({'text': invalid}, WIDTH)
        with self.assertRaises(AssertionError):
            assert_provider_box({'text': text}, WIDTH + 1)

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

    def test_stream_text(self):
        words = [f'm{i:03d}' for i in range(3)]
        text = '◢◤ ask\n  ❯ ' + ' '.join(words) + '\n  ◆ ' + ' '.join(words)
        assert_stream_text(text, 40, 3)
        assert_stream_text(text.rsplit(' ', 1)[0], 40, 3, complete=False)
        for invalid in [text + '\n  ◆ m000', text.replace('m002', 'm001', 1), text.rsplit(' ', 1)[0],
                        text.replace('◢◤ ask', '◢◤ ask\n◢◤ ask'), text + ' ' * 40]:
            with self.subTest(text=invalid), self.assertRaises(AssertionError):
                assert_stream_text(invalid, 40, 3)
        with self.assertRaises(AssertionError):
            assert_stream_text(text.replace('m001', 'm002'), 40, 3, complete=False)

    def test_one_draft(self):
        state = {'text': '╭──╮\n│ ❯ draft  │\n╰──╯', 'cursor': {'x': 9, 'y': 1}}
        assert_one_draft(state, 'draft')
        for invalid in [dict(state, cursor={'x': 8, 'y': 1}), dict(state, cursor={'x': 9, 'y': 0}),
                        dict(state, text=state['text'] + '\ndraft'), dict(state, text='╭──╮\n│ ❯ draf  │')]:
            with self.subTest(state=invalid), self.assertRaises(AssertionError):
                assert_one_draft(invalid, 'draft')

    def test_rebuild_once(self):
        words = ' '.join(f'm{i:03d}' for i in range(3))
        head = '\x1b[1;38;5;253;48;5;235m' + words + '\x1b[m'
        raw = 'old ' + words + PURGE + '◢◤ ask ' + head
        assert_rebuild_once(raw, 3)
        for invalid in ['old ' + words, raw + PURGE + '◢◤ ask ' + head, raw.replace('m001', 'm002'),
                        raw.replace('◢◤ ask', 'x')]:
            with self.subTest(raw=invalid), self.assertRaises(AssertionError):
                assert_rebuild_once(invalid, 3)

    def test_alt_screen(self):
        assert_no_alt_screen('plain')
        for sequence in ['\x1b[?47h', '\x1b[?1047h', '\x1b[?1049h']:
            with self.subTest(sequence=sequence), self.assertRaises(AssertionError):
                assert_no_alt_screen('a' + sequence)

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


if __name__ == '__main__':
    unittest.main()
