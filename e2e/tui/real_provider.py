#!/usr/bin/env python3
"""PTY driver for the real-provider acceptance run of the built `ask`.

Keys reach `ask` only through the inherited environment. The driver never reads
or prints credential values; screens are saved as plain text.

Usage: real_provider.py <scenario> [--cols N --rows N]
Needs pyte (pip install --target /tmp/ask-rp-py pyte; PYTHONPATH=/tmp/ask-rp-py).
"""
import argparse
import fcntl
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import threading
import time

import pyte

ASK = os.environ.get("ASK_BIN", "/tmp/ask-rp-bin/ask")
OUT = os.environ.get("RP_OUT", "/tmp/ask-rp-out")
os.makedirs(OUT, exist_ok=True)


class Session:
    def __init__(self, home, cols=80, rows=24, env=None, args=()):
        self.cols, self.rows = cols, rows
        self.screen = pyte.HistoryScreen(cols, rows, history=2000, ratio=0.5)
        self.stream = pyte.ByteStream(self.screen)
        self.lock = threading.Lock()
        e = dict(os.environ)
        for k in [k for k in e if k.startswith("ASK_")]:
            del e[k]
        e.update({"ASK_HOME": home, "TERM": "xterm-256color", "LANG": "en_US.UTF-8", "COLORTERM": "truecolor"})
        if env:
            e.update(env)
        self.home = home
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.proc = subprocess.Popen([ASK, *args], stdin=slave, stdout=slave, stderr=slave,
                                     env=e, start_new_session=True, close_fds=True, cwd="/tmp")
        os.close(slave)
        self.fd = master
        self.screen.write_process_input = lambda d: os.write(self.fd, d.encode())
        self.alive = True
        self.raw = b""
        threading.Thread(target=self._read, daemon=True).start()

    def _read(self):
        while self.alive:
            r, _, _ = select.select([self.fd], [], [], 0.1)
            if not r:
                continue
            try:
                d = os.read(self.fd, 65536)
            except OSError:
                break
            if not d:
                break
            with self.lock:
                self.raw += d
                self.stream.feed(d)
        self.alive = False

    def send(self, s):
        os.write(self.fd, s.encode() if isinstance(s, str) else s)

    def type(self, s, delay=0.01):
        for ch in s:
            self.send(ch)
            time.sleep(delay)

    def text(self, history=True):
        with self.lock:
            rows = []
            if history:
                for line in self.screen.history.top:
                    rows.append("".join(line[x].data for x in range(self.cols)).rstrip())
            rows += [r.rstrip() for r in self.screen.display]
        while rows and not rows[-1]:
            rows.pop()
        return "\n".join(rows)

    def view(self):
        with self.lock:
            return "\n".join(r.rstrip() for r in self.screen.display)

    def wait(self, pat, timeout=30, view=True):
        end = time.time() + timeout
        rx = re.compile(pat)
        while time.time() < end:
            t = self.view() if view else self.text()
            if rx.search(t):
                return True
            time.sleep(0.1)
        return False

    def settle(self, quiet=1.5, timeout=60):
        """Wait until the screen is unchanged for `quiet` seconds."""
        last, since, end = None, time.time(), time.time() + timeout
        while time.time() < end:
            v = self.view()
            if v != last:
                last, since = v, time.time()
            elif time.time() - since >= quiet:
                return True
            time.sleep(0.1)
        return False

    def resize(self, cols, rows):
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        with self.lock:
            self.screen.resize(rows, cols)
        self.cols, self.rows = cols, rows
        os.killpg(self.proc.pid, signal.SIGWINCH)

    def close(self):
        if self.proc.poll() is None:
            try:
                os.killpg(self.proc.pid, signal.SIGTERM)
                self.proc.wait(5)
            except Exception:
                try:
                    os.killpg(self.proc.pid, signal.SIGKILL)
                except Exception:
                    pass
        self.alive = False
        try:
            os.close(self.fd)
        except OSError:
            pass


def save(name, sess, tail=24):
    p = os.path.join(OUT, name + ".txt")
    t = sess.view()
    with open(p, "w") as f:
        f.write(t + "\n")
    return t


def leader_stop(home):
    return subprocess.run([ASK, "leader", "stop"], env={**os.environ, "ASK_HOME": home}, capture_output=True, text=True, timeout=20)


KEYS = {"esc": "\x1b", "ctrl+c": "\x03", "ctrl+d": "\x04", "ctrl+l": "\x0c", "ctrl+t": "\x14", "enter": "\r",
        "up": "\x1b[A", "down": "\x1b[B"}

if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("home")
    ap.add_argument("--cols", type=int, default=80)
    ap.add_argument("--rows", type=int, default=24)
    ap.add_argument("--seconds", type=float, default=6)
    a = ap.parse_args()
    s = Session(a.home, a.cols, a.rows)
    time.sleep(a.seconds)
    print(s.view())
    s.send("\x04")
    time.sleep(1)
    s.close()
    leader_stop(a.home)
