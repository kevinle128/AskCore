"""Verify native resize while the renderer owns a held transcript write."""
import json
import hashlib
import os
from pathlib import Path
import re
import select
import sys
import time
import uuid

from run import Session, assert_transcript

PURGE = '\x1b[2J\x1b[3J\x1b[H'
MARKER = re.compile(r'G1\[\d{4}\]')


def pending_resize(cli, backend, target, directory, artifacts, capacity_probe=False):
    artifacts = Path(artifacts)
    artifacts.mkdir(parents=True, exist_ok=True)
    paths = [artifacts / 'control.fifo', artifacts / 'status.fifo']
    descriptors = []
    session = None
    lines = []
    buffer = b''
    try:
        for path in paths:
            os.mkfifo(path, 0o600)
            descriptors.append(os.open(path, os.O_RDWR | os.O_NONBLOCK))
        control, status = descriptors
        wrapper = artifacts / 'launch.py'
        # Duplicate above reserved FDs before mapping, so FD 3/4 cannot collide.
        wrapper.write_text(
            'import os,sys,fcntl\n'
            f'paths={list(map(str, paths))!r}\n'
            'fds=[os.open(p,os.O_RDWR) for p in paths]\n'
            'safe=[fcntl.fcntl(fd,fcntl.F_DUPFD,10) for fd in fds]\n'
            'for fd in fds:os.close(fd)\n'
            'for fd,dest in zip(safe,(3,4)):\n'
            ' os.dup2(fd,dest,inheritable=True)\n'
            ' os.close(fd)\n'
            'env=dict(os.environ,T0_CONTROL="1",T0_TERMINAL_REFLOW="native")\n'
            f'os.execve({str(target)!r},[{str(target)!r}]+sys.argv[1:],env)\n'
        )
        wrapper.chmod(0o700)
        executable = artifacts / 'launch'
        executable.write_text(f'#!{sys.executable}\n' + wrapper.read_text())
        executable.chmod(0o700)

        def wait_status(predicate, timeout=15):
            nonlocal buffer
            deadline = time.monotonic() + timeout
            while True:
                for line in lines:
                    if predicate(line):
                        return line
                remaining = deadline - time.monotonic()
                assert remaining > 0, f'status timeout: {lines[-12:]}'
                ready, _, _ = select.select([status], [], [], remaining)
                if not ready:
                    continue
                chunk = os.read(status, 65536)
                assert chunk, 'status pipe closed'
                buffer += chunk
                while b'\n' in buffer:
                    line, buffer = buffer.split(b'\n', 1)
                    lines.append(line.decode())

        def send(text):
            payload = (text + '\n').encode()
            assert os.write(control, payload) == len(payload), 'short control write'

        session = Session(cli, 'askcore-pending-' + uuid.uuid4().hex[:12],
                          backend, executable, Path(directory), artifacts / 'session', 'g1')
        session.write('draft')
        session.cursor(13, 'editor> draft')
        send('g1-pending-resize')
        wait_status(lambda line: line == 'pending-write')
        # The terminal changes before the held write resumes; no model message is injected.
        width, height = (52, 15) if capacity_probe else (13, 6)
        session.resize(width, height)
        send('release-write')
        wait_status(lambda line: line == f'size {width} {height}')
        wait_status(lambda line: line == 'commit 2000 2000 false')
        wait_status(lambda line: line.startswith('replay '))
        session.wait(lambda state: 'tail 2000' in state['text'])
        assert_transcript(session.text())
        if not capacity_probe:
            generation = max(int(line.split()[1]) for line in lines if line.startswith('replay '))
            session.resize(40, 12)
            wait_status(lambda line: line == 'size 40 12')
            wait_status(lambda line: line.startswith('replay ') and int(line.split()[1]) > generation)
        session.cursor(13, 'editor> draft')
        assert_transcript(session.text())

        replay_status = [tuple(map(int, line.split()[1:])) for line in lines if line.startswith('replay ')]
        expected_sizes = [(52, 15)] if capacity_probe else [(13, 6), (40, 12)]
        assert [item[4:] for item in replay_status] == expected_sizes, 'replay used stale dimensions'
        replays = [item[:4] for item in replay_status]
        replay_ids = [item[0] for item in replays]
        assert replay_ids == sorted(set(replay_ids)), 'duplicate or stale replay generation'
        raw = session.recording()
        generations = raw.split(PURGE)
        assert len(generations) == len(replays) + 1, 'replay status/output generation mismatch'
        ordinary = MARKER.findall(generations[0])
        replay_counts = []
        for generation in generations:
            for forbidden in ('\x1b[2J', '\x1b[3J', '\x1b[?1049h', '\x1b[?47h', '\x1b[?1047h'):
                assert forbidden not in generation, f'isolated purge or alternate buffer: {forbidden!r}'
        for generation, (_, entries, confirmed, scheduled) in zip(generations[1:], replays):
            count = entries - 1  # Startup is retained but is not a G1 commit.
            assert count == confirmed == scheduled, 'replay changed the commit frontier'
            ids = MARKER.findall(generation)
            expected = [f'G1[{i:04d}]' for i in range(count)]
            assert ids[:count] == expected, 'partial or reordered replay snapshot'
            ordinary.extend(ids[count:])
            replay_counts.append(count)
        assert ordinary == [f'G1[{i:04d}]' for i in range(2000)], 'ordinary commits not exact-once'
        commits = [int(line.split()[2]) for line in lines if line.startswith('commit ')]
        assert commits == list(range(1, 2001)), 'duplicate or missing commit acknowledgements'
        assert '\x1b[?1049h' not in raw, 'alternate screen entered'
        session.capture('pending-resize')
        session.key('ctrl+c')
        wait_status(lambda line: line == 'restored')
        send('exit')
        session.call('wait', 'exit', '--timeout', '5000')
        report = json.loads(session.report.read_text())
        assert report['SHA256'] == hashlib.sha256(report['Text'].encode()).hexdigest(), 'draft hash differs'
        assert report['Text'] == 'draft' and report['Cursor'] == 5, 'draft changed'
        return {'ordinary_commits': 2000, 'replay_snapshot_commits': replay_counts,
                'real_pending_write_resize': 'pass'}
    except BaseException:
        if session is not None:
            session.capture('failure')
        raise
    finally:
        try:
            if session is not None:
                session.close()
        finally:
            for fd in descriptors:
                os.close(fd)
            for path in paths:
                if path.exists():
                    path.unlink()
            (artifacts / 'status-lines.json').write_text(json.dumps(lines, indent=2) + '\n')
