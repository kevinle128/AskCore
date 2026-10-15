FROM golang:1.27-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends libfuse3-dev fuse3 build-essential pkg-config && rm -rf /var/lib/apt/lists/*
COPY fsync-gate.c /tmp/fsync-gate.c
RUN cc -std=c11 -Wall -Wextra -Werror -O2 /tmp/fsync-gate.c $(pkg-config --cflags --libs fuse3) -pthread -o /usr/local/bin/ask-fsync-gate && rm /tmp/fsync-gate.c
ENV ASK_FSYNC_GATE_BINARY=/usr/local/bin/ask-fsync-gate
WORKDIR /workspace
