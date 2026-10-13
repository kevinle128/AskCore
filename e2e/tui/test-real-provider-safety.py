"""Run safety controls with fake sessions and no provider calls."""
import ast
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
from types import SimpleNamespace
import unittest


def load_helpers(root, events):
    source = Path(__file__).with_name('real_provider_scenarios.py').read_text()
    tree = ast.parse(source)
    names = {'fresh_home', 'start', 'stop_home', 'cleanup', 'stored_strings', 'artifact_scan'}
    tree.body = [node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name in names]

    class FakeSession:
        def __init__(self, home, *args, **kwargs):
            self.home = home

        def wait(self, *args):
            return True

        def close(self):
            events.append('close')

    def stop(home):
        events.append('stop')
        return type('Result', (), {'returncode': 0})()

    class Scanner:
        def scan_secrets(self, output, secrets):
            events.append('scan')
            assert Path(root, 'key', 'auth.json').exists()
            assert secrets['auth.providers.openai.oauth.accessToken'] == 'fake-refreshed-token'
            return {}

    scope = dict(os=os, shutil=shutil, json=json, Session=FakeSession, leader_stop=stop,
                 RUN_HOME=root, KEY_HOME=str(Path(root, 'key')), OAUTH_HOME=str(Path(root, 'oauth')),
                 SESSIONS=[], STARTED_HOMES=set(), SCAN_SET={}, OUT=str(Path(root, 'output')),
                 RESULTS=[], CALLS={}, runner=Scanner(), log=lambda msg: None, check=lambda *args: None)
    Path(scope['OUT']).mkdir()
    exec(compile(tree, 'real-provider-safety-helpers', 'exec'), scope)
    return scope


class SafetyControls(unittest.TestCase):
    def test_session_close_waits_after_a_forced_stop(self):
        source = Path(__file__).with_name('real_provider.py').read_text()
        tree = ast.parse(source)
        tree.body = [node for node in tree.body if isinstance(node, ast.ClassDef) and node.name == 'Session']
        events = []

        class Process:
            pid = 123
            waits = 0

            def poll(self):
                return None

            def wait(self, timeout):
                self.waits += 1
                events.append('wait')
                if self.waits == 1:
                    raise subprocess.TimeoutExpired('fake-child', timeout)

        safe_os = SimpleNamespace(killpg=lambda pid, sig: events.append(sig), close=lambda fd: None)
        scope = dict(os=safe_os, signal=signal, subprocess=subprocess)
        exec(compile(tree, 'real-provider-session-control', 'exec'), scope)
        session = scope['Session'].__new__(scope['Session'])
        session.proc, session.fd, session.alive = Process(), -1, True
        session.reader = SimpleNamespace(join=lambda timeout: events.append('join'))
        session.close()
        self.assertEqual(events, [signal.SIGTERM, 'wait', signal.SIGKILL, 'wait', 'join'])

    def test_cleanup_stops_owned_sessions_and_scans_new_tokens_before_removal(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = str(Path(temporary, 'run'))
            Path(root).mkdir()
            events = []
            scope = load_helpers(root, events)
            home = Path(scope['KEY_HOME'])
            home.mkdir()
            (home / 'auth.json').write_text(json.dumps({'providers': {'openai': {
                'oauth': {'accessToken': 'fake-refreshed-token'}}}}))
            scope['start'](str(home), 80, 24)
            scope['cleanup']()
            self.assertEqual(events, ['close', 'stop', 'scan'])
            self.assertFalse(Path(root).exists())

    def test_home_reset_rejects_an_unowned_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = str(Path(temporary, 'run'))
            Path(root).mkdir()
            scope = load_helpers(root, [])
            other = Path(temporary, 'other')
            other.mkdir()
            marker = other / 'keep.txt'
            marker.write_text('keep')
            with self.assertRaises(ValueError):
                scope['fresh_home'](str(other))
            self.assertTrue(marker.exists())


if __name__ == '__main__':
    unittest.main()
