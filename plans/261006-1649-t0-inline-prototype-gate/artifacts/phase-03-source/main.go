package main

import (
	"bufio"
	tea "charm.land/bubbletea/v2"
	"context"
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

func main() { os.Exit(run()) }
func run() int {
	scenario := flag.String("scenario", "smoke", "scenario")
	keyboard := flag.String("keyboard", "auto", "auto or off")
	lines := flag.Int("lines", 2000, "manual transcript lines (1..9999)")
	flag.Parse()
	if *lines < 1 || *lines > 9999 {
		fmt.Fprintln(os.Stderr, "lines must be 1..9999")
		return 2
	}
	if (*scenario != "smoke" && *scenario != "g6" && *scenario != "g1") || (*keyboard != "auto" && *keyboard != "off") {
		fmt.Fprintln(os.Stderr, "unknown scenario or keyboard mode")
		return 2
	}
	var status, control *os.File
	if os.Getenv("T0_CONTROL") == "1" {
		if err := unix.SetNonblock(4, true); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		control = os.NewFile(3, "control")
		status = os.NewFile(4, "status")
		defer control.Close()
		defer status.Close()
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()
	writer := &faultOutput{File: os.Stdout, errors: make(chan error, 1), commits: make(chan commitWrite, 16), pause: make(chan struct{}, 1), done: ctx.Done(), status: status}
	p := tea.NewProgram(model{manualTranscript: *scenario == "g1" && control == nil, transcriptLines: *lines, g1: *scenario == "g1", status: status, g6: *scenario == "g6", keyboardOff: *keyboard == "off"}, tea.WithContext(ctx), tea.WithoutSignalHandler(), tea.WithOutput(writer))
	exit := make(chan struct{})
	var workers sync.WaitGroup
	if control != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer close(exit)
			s := bufio.NewScanner(control)
			for s.Scan() {
				switch s.Text() {
				case "quit":
					p.Quit()
				case "cancel":
					cancel()
				case "output-error", "output-permanent":
					writer.arm(s.Text() == "output-permanent")
					p.Send(fixtureAction("output-error"))
				case "g1-pending-resize":
					writer.holdNext()
					p.Send(fixtureAction("g1-start"))
				case "release-write":
					writer.release()
				case "g1-fail-sync":
					writer.arm(false)
					p.Send(fixtureAction("output-error"))
				case "g1-fail-zero", "g1-fail-partial", "g1-fail-short", "g1-fail-permanent":
					writer.armCommit(strings.TrimPrefix(s.Text(), "g1-fail-"))
					p.Send(fixtureAction("g1-start"))
				case "g1-stream", "g1-continue", "g1-shrink-prepare", "g1-release", "g1-start", "g1-oversized", "g1-out-of-order", "g1-shrink":
					p.Send(fixtureAction(s.Text()))
				case "ready 0":
					p.Send(readyBlock(0))
				case "ready 1":
					p.Send(readyBlock(1))
				case "ready 2":
					p.Send(readyBlock(2))
				case "inspect":
					p.Send(inspectDraft{})
				case "model-panic", "command-panic":
					p.Send(fixtureAction(s.Text()))
				case "exit":
					return
				}
			}
			if err := s.Err(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}()
	}
	watchDone := make(chan struct{})
	watchStop := make(chan struct{})
	var outputErr error
	go func() {
		defer close(watchDone)
		select {
		case outputErr = <-writer.errors:
			cancel()
		case <-watchStop:
		}
	}()
	commitDone := make(chan struct{})
	commitStop := make(chan struct{})
	go func() {
		defer close(commitDone)
		for {
			select {
			case event := <-writer.commits:
				p.Send(event)
			case <-commitStop:
				return
			}
		}
	}()
	finalModel, err := p.Run()
	close(commitStop)
	<-commitDone
	if m, ok := finalModel.(model); ok && m.g1 {
		writer.mu.Lock()
		m.transcript.stopped = writer.failed
		writer.mu.Unlock()
		m.commitReport()
	}
	close(watchStop)
	<-watchDone
	if outputErr != nil {
		err = outputErr
	}
	cleanupErr := writer.cleanup()
	if reportErr := writer.report(status); reportErr != nil {
		fmt.Fprintln(os.Stderr, reportErr)
	}
	if cleanupErr != nil {
		fmt.Fprintln(os.Stderr, "terminal byte restoration unavailable:", cleanupErr)
	}
	code := 0
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	if status != nil {
		if err := writeStatus(status, "%s\n", func() string {
			if cleanupErr != nil {
				return "termios-restored output-unavailable"
			}
			return "restored"
		}()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			control.Close()
			workers.Wait()
			return 1
		}
		<-exit
		workers.Wait()
	}
	return code
}
