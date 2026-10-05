#!/usr/bin/env bash
# Launch, check, and stop an isolated ask-server for verification.
# Usage: verify-ask-server.sh launch [RUN_ID]
#        verify-ask-server.sh doctor RUN_ID
#        verify-ask-server.sh cleanup RUN_ID
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
SKILL_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
REPO=$(cd "$SKILL_DIR/../../.." && pwd)

usage() {
  echo "usage: verify-ask-server.sh launch [RUN_ID] | doctor RUN_ID | cleanup RUN_ID" >&2
  exit 2
}

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

require_run_id() {
  if [[ -z "${RUN_ID}" ]]; then
    echo "RUN_ID is required" >&2
    usage
  fi
}

paths() {
  EVIDENCE="$SKILL_DIR/evidence/$RUN_ID"
  SCRATCH="${TMPDIR:-/tmp}/askcore-verify-$RUN_ID"
  LAUNCH_ENV="$EVIDENCE/launch.env"
}

cmd=${1:-}
RUN_ID=${2:-${ASK_VERIFY_RUN_ID:-}}
case "$cmd" in
  launch|doctor|cleanup) ;;
  *) usage ;;
esac

if [[ "$cmd" == "launch" && -z "$RUN_ID" ]]; then
  RUN_ID=$(date +%Y%m%d%H%M%S)
fi
require_run_id
paths

case "$cmd" in
  launch)
    mkdir -p "$EVIDENCE" "$SCRATCH"
    HTTP_PORT=$(free_port)
    GRPC_PORT=$(free_port)
    if [[ "$HTTP_PORT" == "$GRPC_PORT" ]]; then
      GRPC_PORT=$(free_port)
    fi
    (
      cd "$REPO"
      go build -o "$SCRATCH/ask-server" ./cmd/server
    )
    # Double-fork into a new session. A background job of this script is
    # killed when the launching shell's process group exits.
    env \
      HOST=127.0.0.1 \
      PORT="$HTTP_PORT" \
      GRPC_PORT="$GRPC_PORT" \
      DATABASE_URL="$SCRATCH/app.db" \
      LOG_LEVEL=debug \
      CORS_ORIGIN='*' \
      python3 - "$SCRATCH/ask-server" "$EVIDENCE/server.log" "$EVIDENCE/server.pid" <<'PY'
import os, sys
binary, log, pidfile = sys.argv[1:4]
if os.fork() > 0:
    os.wait()
    raise SystemExit(0)
os.setsid()
if os.fork() > 0:
    raise SystemExit(0)
fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o644)
os.dup2(fd, 1)
os.dup2(fd, 2)
os.close(fd)
with open(pidfile, "w") as fh:
    fh.write(str(os.getpid()))
os.execv(binary, [binary])
PY
    BASE_URL="http://127.0.0.1:$HTTP_PORT"
    for _ in $(seq 1 50); do
      [[ -s "$EVIDENCE/server.pid" ]] && break
      sleep 0.05
    done
    if [[ ! -s "$EVIDENCE/server.pid" ]]; then
      echo "ask-server did not write a pid. See $EVIDENCE/server.log" >&2
      exit 1
    fi
    ready=0
    for _ in $(seq 1 50); do
      if curl -fsS "$BASE_URL/health" >/dev/null 2>&1; then
        ready=1
        break
      fi
      if ! kill -0 "$(cat "$EVIDENCE/server.pid")" 2>/dev/null; then
        echo "ask-server exited before /health. See $EVIDENCE/server.log" >&2
        exit 1
      fi
      sleep 0.1
    done
    if [[ "$ready" != 1 ]]; then
      echo "timed out waiting for $BASE_URL/health. See $EVIDENCE/server.log" >&2
      kill -TERM "$(cat "$EVIDENCE/server.pid")" 2>/dev/null || true
      exit 1
    fi
    REV=$(git -C "$REPO" rev-parse --short HEAD 2>/dev/null || echo unknown)
    cat >"$LAUNCH_ENV" <<EOF
RUN_ID=$RUN_ID
REPO=$REPO
BASE_URL=$BASE_URL
HOST=127.0.0.1
PORT=$HTTP_PORT
GRPC_ADDR=127.0.0.1:$GRPC_PORT
DATABASE_URL=$SCRATCH/app.db
PID=$(cat "$EVIDENCE/server.pid")
BINARY=$SCRATCH/ask-server
EVIDENCE=$EVIDENCE
SCRATCH=$SCRATCH
GIT_REV=$REV
EOF
    echo "RUN_ID=$RUN_ID"
    echo "BASE_URL=$BASE_URL"
    echo "GRPC_ADDR=127.0.0.1:$GRPC_PORT"
    echo "DATABASE_URL=$SCRATCH/app.db"
    echo "EVIDENCE=$EVIDENCE"
    ;;
  doctor)
    if [[ ! -f "$LAUNCH_ENV" ]]; then
      echo "no launch.env for $RUN_ID" >&2
      exit 1
    fi
    # shellcheck disable=SC1090
    source "$LAUNCH_ENV"
    if [[ "$HOST" != "127.0.0.1" ]]; then
      echo "refusing to drive HOST=$HOST" >&2
      exit 1
    fi
    if ! kill -0 "$PID" 2>/dev/null; then
      echo "pid $PID is not running" >&2
      exit 1
    fi
    comm=$(ps -p "$PID" -o command=)
    case "$comm" in
      "$BINARY"*) ;;
      *)
        echo "pid $PID is not $BINARY (command: $comm)" >&2
        exit 1
        ;;
    esac
    listen_pid=$(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t | head -n 1)
    if [[ "$listen_pid" != "$PID" ]]; then
      echo "port $PORT is owned by ${listen_pid:-nobody}, not $PID" >&2
      exit 1
    fi
    health=$(curl -fsS "$BASE_URL/health")
    echo "$health" | grep -q '"status":"ok"'
    echo "$health" | grep -q '"message":"Server is running"'
    if [[ ! -f "$DATABASE_URL" ]]; then
      echo "database missing: $DATABASE_URL" >&2
      exit 1
    fi
    case "$DATABASE_URL" in
      "$SCRATCH"/*) ;;
      *)
        echo "database is not in this run's scratch dir: $DATABASE_URL" >&2
        exit 1
        ;;
    esac
    echo "doctor ok RUN_ID=$RUN_ID BASE_URL=$BASE_URL GIT_REV=$GIT_REV"
    ;;
  cleanup)
    if [[ -f "$EVIDENCE/server.pid" ]]; then
      pid=$(cat "$EVIDENCE/server.pid")
      if kill -0 "$pid" 2>/dev/null; then
        kill -TERM "$pid" 2>/dev/null || true
        for _ in $(seq 1 30); do
          kill -0 "$pid" 2>/dev/null || break
          sleep 0.1
        done
        if kill -0 "$pid" 2>/dev/null; then
          kill -KILL "$pid" 2>/dev/null || true
        fi
      fi
    fi
    if [[ -d "$SCRATCH" ]]; then
      rm -rf "$SCRATCH"
    fi
    echo "cleaned RUN_ID=$RUN_ID (evidence kept at $EVIDENCE)"
    ;;
esac
