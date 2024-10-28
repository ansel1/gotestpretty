package main

// A simple program demonstrating the spinner component from the Bubbles
// component library.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
)

var flags struct {
	replay         bool
	rate           float64
	infile         string
	includePassed  bool
	includeSkipped bool
	includeSlow    bool
	slowThreshold  time.Duration
	noTTY          bool
}

func parseFlags() {
	flag.BoolVar(&flags.replay, "replay", false, "Replay events with pauses to simulate original test run")
	flag.Float64Var(&flags.rate, "rate", 1, "Set rate to replay, defaults to 1 (original speed), 0.5 = double speed, 0 = no pauses, ignored unless --replay=true")
	flag.StringVar(&flags.infile, "f", "", "Read from <filename> instead of stdin")
	flag.BoolVar(&flags.includePassed, "include-passed", false, "Include passed tests")
	flag.BoolVar(&flags.includeSlow, "include-slow", false, "Include slow tests")
	flag.BoolVar(&flags.includeSkipped, "include-skipped", true, "Include skipped tests")
	flag.DurationVar(&flags.slowThreshold, "slow-threshold", time.Second, "Set slow threshold")
	flag.BoolVar(&flags.noTTY, "notty", false, "Don't open a tty (not typically needed)")

	flag.Parse()
}

func main() {
	parseFlags()

	m := newModel()
	var p *tea.Program
	if flags.noTTY {
		p = tea.NewProgram(m, tea.WithInput(nil))
	} else {
		p = tea.NewProgram(m)
	}

	m.prog = p

	go process(p)

	if _, err := p.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if m.fails > 0 {
		os.Exit(1)
	}
}

// process reads the input until EOF.
// Lines which appear to be gotest output are sent to the event loop for
// further processing and rendering.  Other lines are just dumped to
// the terminal output.
func process(p *tea.Program) {
	var r io.Reader
	if flags.infile != "" {
		f, err := os.Open(flags.infile)
		if err != nil {
			p.Send(err)
			return
		}
		r = f
	} else {
		r = os.Stdin
	}
	r = bufio.NewReader(r)

	var lastTs time.Time

	s := bufio.NewScanner(r)
	for s.Scan() {
		var e TestEvent
		decoder := json.NewDecoder(bytes.NewReader(s.Bytes()))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&e)
		if err != nil {
			// this line wasn't valid json, so just print it
			p.Println(s.Text())
			continue
		}

		// replay support: injects sleeps to simulate the original
		// timing of the test output
		if flags.replay {
			if !lastTs.IsZero() && !e.Time.IsZero() {
				pause := e.Time.Sub(lastTs)
				pause = time.Duration(float64(pause) * flags.rate)
				time.Sleep(pause)
			}
			lastTs = e.Time
		}

		p.Send(e)
	}
	p.Send(Done{})
}
