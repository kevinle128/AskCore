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
	"sync"
	"syscall"
)

func main() { os.Exit(run()) }
func run() int {
	scenario := flag.String("scenario", "smoke", "scenario")
	keyboard := flag.String("keyboard", "auto", "auto or off")
	flag.Parse()
	if (*scenario != "smoke" && *scenario != "g6") || (*keyboard != "auto" && *keyboard != "off") {
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
	writer := &faultOutput{File: os.Stdout, errors: make(chan error, 1)}
	p := tea.NewProgram(model{status: status, g6: *scenario == "g6", keyboardOff: *keyboard == "off"}, tea.WithContext(ctx), tea.WithoutSignalHandler(), tea.WithOutput(writer))
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
	_, err := p.Run()
	close(watchStop)
	<-watchDone
	if outputErr != nil {
		err = outputErr
	}
	cleanupErr := writer.cleanup()
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
