package main

import (
	"bytes"
	"strings"
	"time"
)

type node struct {
	name      string
	start     time.Time
	status    string
	children  []node
	parent    *node
	outputBuf *bytes.Buffer
	elapsed   time.Duration
	isTest    bool
}

func (n *node) output(s string) {
	if strings.HasPrefix(s, "===") {
		// e.g. === RUN, === PAUSE, === CONT
		// skip it
		return
	}
	if strings.HasPrefix(s, "---") {
		// e.g. --- PASS and --- FAIL
		n.prepend(s)
		return
	}

	n.append(s)
}

func (n *node) append(s string) {
	if n.outputBuf == nil {
		n.outputBuf = bytes.NewBufferString(s)
	} else {
		n.outputBuf.WriteString(s)
	}
}

func (n *node) prepend(s string) {
	buf := bytes.NewBufferString(s)
	if n.outputBuf != nil {
		_, _ = n.outputBuf.WriteTo(buf)
	}
	n.outputBuf = buf
}
