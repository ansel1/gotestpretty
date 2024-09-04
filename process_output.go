package main

import (
	"bufio"
	"encoding/json"
	"github.com/ansel1/merry/v2"
	"io"
	"time"
)

type TestEvent struct {
	Time    time.Time // encodes as an RFC3339-format string
	Action  string
	Package string
	Test    string
	Elapsed float64 // seconds
	Output  string
	orig    string
}

type Processor struct {
	r       io.Reader
	onEvent func(e TestEvent)
}

func NewProcessor(r io.Reader) *Processor {
	return &Processor{r: r}
}

func (p *Processor) Process() error {
	s := bufio.NewScanner(p.r)
	for s.Scan() {
		var e TestEvent
		err := json.Unmarshal(s.Bytes(), &e)
		if err != nil {
			return merry.Prepend(err, "failed to parse test event")
		}
		e.orig = s.Text()

		if p.onEvent != nil {
			p.onEvent(e)
		}
	}
	return nil
}
