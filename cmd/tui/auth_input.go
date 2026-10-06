package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const authInputLimit = 16 * 1024

var errPrivateInterrupt = errors.New("private input interrupted")

// readPrivateLine has no detached reader that can survive command cancellation.
func readPrivateLine(ctx context.Context, input io.Reader) (string, error) {
	var file *os.File
	if f, ok := input.(*os.File); ok {
		file = f
	}
	if file != nil && term.IsTerminal(int(file.Fd())) {
		state, err := term.MakeRaw(int(file.Fd()))
		if err != nil {
			return "", err
		}
		defer func() { _ = term.Restore(int(file.Fd()), state) }()
	}
	var b strings.Builder
	var one [1]byte
	readBytes := 0
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if file != nil {
			poll := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
			n, err := unix.Poll(poll, 100)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				return "", err
			}
			if n == 0 {
				continue
			}
		}
		n, err := input.Read(one[:])
		if n > 0 {
			if one[0] != '\n' {
				readBytes++
				if readBytes > authInputLimit {
					return "", errors.New("private input exceeds 16 KiB")
				}
			}
			switch one[0] {
			case '\n':
				return b.String(), nil
			case '\r':
				if file != nil && isTerminal(file) {
					return b.String(), nil
				}
				continue
			case 3:
				if file != nil && isTerminal(file) {
					return "", errPrivateInterrupt
				}
			case 4:
				if file != nil && isTerminal(file) {
					if b.Len() > 0 {
						return b.String(), nil
					}
					return "", io.EOF
				}
			case 8, 127:
				if file != nil && isTerminal(file) {
					text := b.String()
					if len(text) > 0 {
						b.Reset()
						b.WriteString(text[:len(text)-1])
					}
					continue
				}
			}
			b.WriteByte(one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && b.Len() > 0 {
				return b.String(), nil
			}
			return "", err
		}
	}
}
