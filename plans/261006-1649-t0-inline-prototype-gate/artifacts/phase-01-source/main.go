package main

import (
	"bufio"
	tea "charm.land/bubbletea/v2"
	"flag"
	"fmt"
	"os"
	"sync"
)

func main() {
	scenario := flag.String("scenario", "smoke", "scenario")
	flag.Parse()
	if *scenario != "smoke" {
		fmt.Fprintln(os.Stderr, "unknown scenario")
		os.Exit(2)
	}
	var status, control *os.File
	if os.Getenv("T0_CONTROL") == "1" {
		control = os.NewFile(3, "control")
		status = os.NewFile(4, "status")
		defer control.Close()
		defer status.Close()
	}
	p := tea.NewProgram(model{status: status})
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
				case "exit":
					return
				}
			}
		}()
	}
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if status != nil {
		if _, err := fmt.Fprintln(status, "restored"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		<-exit
		workers.Wait()
	}
}
