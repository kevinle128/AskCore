#!/usr/bin/env python3
"""Run-scoped helper for the verify-ask skill.

    askv.py build                    build ./cmd/tui to $ASKV_BIN (default /tmp/askv/ask)
    askv.py new-run [--gated]        make an isolated run; print its JSON (run, home, work, evidence, bin)
    askv.py doctor RUN               read-only check that the run is worth driving
    askv.py acp RUN [--text TEXT]    drive `ask acp` over stdio with the faux provider; save the transcript
    askv.py cleanup RUN              stop this run's leader, delete its home and work; keep the evidence

RUN is the `run` id printed by new-run. Every run owns /tmp/askv-<RUN>/{home,work} (short path:
the leader socket path must stay short) and /tmp/ask-verify-evidence/<RUN>/ (never deleted here).
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[4]
BIN = Path(os.environ.get('ASKV_BIN', '/tmp/askv/ask'))
EVIDENCE_ROOT = Path('/tmp/ask-verify-evidence')


def run_dir(run):
    return Path('/tmp') / f'askv-{run}'


def child_env(run, **extra):
    env = {k: v for k, v in os.environ.items() if not k.startswith('ASK_')}
    for k in list(env):
        if any(s in k.upper() for s in ('API_KEY', 'TOKEN', 'SECRET', 'PASSWORD', 'CREDENTIAL')):
            del env[k]
    env['ASK_HOME'] = str(run_dir(run) / 'home')
    env.update(extra)
    return env


def ask(run, *args, stdin=None, faux=False, cwd=None, timeout=60):
    extra = {'ASK_FAUX': '1'} if faux else {}
    return subprocess.run([str(BIN), *args], input=stdin, capture_output=True, text=True, timeout=timeout,
                          env=child_env(run, **extra), cwd=cwd or run_dir(run) / 'work')


def cmd_build(_):
    BIN.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(['go', 'build', '-o', str(BIN), './cmd/tui'], cwd=ROOT, check=True)
    print(BIN)


def cmd_new_run(args):
    d = Path(tempfile.mkdtemp(prefix='askv-', dir='/tmp'))
    run = d.name[len('askv-'):]
    (d / 'home').mkdir(mode=0o700)
    (d / 'work').mkdir()
    if args.gated:
        (d / 'work' / '.ask').mkdir()
        (d / 'work' / '.ask' / 'settings.json').write_text('{"thinkingFold": "collapsed"}\n')
    evidence = EVIDENCE_ROOT / run
    evidence.mkdir(parents=True)
    print(json.dumps({'run': run, 'home': str(d / 'home'), 'work': str(d / 'work'),
                      'evidence': str(evidence), 'bin': str(BIN)}))


def cmd_doctor(args):
    run, problems = args.run, []
    d = run_dir(run)
    if not d.is_dir():
        sys.exit(f'FAIL no run dir {d}; run new-run first')
    if not BIN.is_file():
        sys.exit(f'FAIL no binary at {BIN}; run build first')
    v = json.loads(ask(run, 'version', '--json').stdout)
    head = subprocess.run(['git', 'rev-parse', '--short=12', 'HEAD'], cwd=ROOT, capture_output=True, text=True).stdout.strip()
    if not v['build'].startswith(head):
        problems.append(f"binary build {v['build']} is not HEAD {head}: rebuild")
    st = ask(run, 'settings', 'show', '--json')
    if st.returncode != 0:
        problems.append(f'ask settings show exit {st.returncode}: {st.stderr.strip()}')
    else:
        home = json.loads(st.stdout)['home']
        if home != str(d / 'home') and os.path.realpath(home) != os.path.realpath(d / 'home'):
            problems.append(f'binary resolves ASK_HOME to {home}, not this run home')
    lead = ask(run, 'leader', 'status')
    if lead.returncode == 0:
        problems.append('a leader already runs for this home; the run is not fresh')
    if shutil.which('tui-test') is None:
        print('WARN tui-test missing: the TUI journey needs e2e/tui/run.py prerequisites')
    print(f"build={v['build']} head={head} home={d / 'home'} binary_ok={not problems}")
    if problems:
        sys.exit('FAIL ' + '; '.join(problems))
    print('OK')


def cmd_acp(args):
    run = args.run
    d = run_dir(run)
    evidence = EVIDENCE_ROOT / run
    work = str(d / 'work')
    p = subprocess.Popen([str(BIN), 'acp'], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                         text=True, env=child_env(run, ASK_FAUX='1'), cwd=work)
    log = []

    def send(i, method, params):
        line = json.dumps({'jsonrpc': '2.0', 'id': i, 'method': method, 'params': params})
        log.append({'dir': 'out', 'msg': json.loads(line)})
        p.stdin.write(line + '\n')
        p.stdin.flush()
        while True:
            raw = p.stdout.readline()
            if not raw:
                sys.exit(f'FAIL ask acp closed stdout before answering {method}')
            msg = json.loads(raw)
            log.append({'dir': 'in', 'msg': msg})
            if msg.get('id') == i:
                return msg

    send(1, 'initialize', {'protocolVersion': 1})
    sid = send(2, 'session/new', {'cwd': work, 'mcpServers': []})['result']['sessionId']
    reply = send(3, 'session/prompt', {'sessionId': sid, 'prompt': [{'type': 'text', 'text': args.text}]})
    p.stdin.close()
    code = p.wait(timeout=30)
    (evidence / 'acp-transcript.jsonl').write_text(''.join(json.dumps(e) + '\n' for e in log))
    updates = [e['msg']['params']['update'] for e in log if e['dir'] == 'in' and e['msg'].get('method') == 'session/update']
    kinds = sorted({u.get('sessionUpdate', '?') for u in updates})
    text = json.dumps(updates)
    stop = reply.get('result', {}).get('stopReason')
    ok = stop == 'end_turn' and code == 0 and args.text.removeprefix('echo ') in text
    print(json.dumps({'stopReason': stop, 'exit': code, 'update_kinds': kinds, 'updates': len(updates), 'ok': ok}))
    if not ok:
        sys.exit('FAIL acp journey; see ' + str(evidence / 'acp-transcript.jsonl'))


def cmd_cleanup(args):
    run = args.run
    d = run_dir(run)
    if not d.is_dir():
        print('nothing to clean')
        return
    if BIN.is_file():
        ask(run, 'leader', 'stop')
        time.sleep(0.2)
        left = ask(run, 'leader', 'status')
        if left.returncode == 0:
            sys.exit('FAIL leader for this run still alive after stop; kill the PID in `ask leader status` by hand')
    shutil.rmtree(d)
    print(f'removed {d}; evidence kept at {EVIDENCE_ROOT / run}')


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest='cmd', required=True)
    sub.add_parser('build').set_defaults(fn=cmd_build)
    n = sub.add_parser('new-run')
    n.add_argument('--gated', action='store_true', help='put .ask/settings.json in the work folder (trust gate)')
    n.set_defaults(fn=cmd_new_run)
    for name, fn in (('doctor', cmd_doctor), ('cleanup', cmd_cleanup)):
        sub.add_parser(name).add_argument('run')
        sub.choices[name].set_defaults(fn=fn)
    a = sub.add_parser('acp')
    a.add_argument('run')
    a.add_argument('--text', default='echo hi')
    a.set_defaults(fn=cmd_acp)
    args = ap.parse_args()
    args.fn(args)


if __name__ == '__main__':
    main()
