package main

import (
	"bytes"
	"container/list"
	"regexp"
	"strings"
	"time"
)

type node struct {
	name      string
	start     time.Time
	status    string
	done      bool
	children  *list.List
	parent    *node
	outputBuf *bytes.Buffer
	elapsed   time.Duration
	isTest    bool
	lvl       int
	msg       string
}

var packageSummaryPattern = regexp.MustCompile(`^(.{4})?\t\S+\t([^\s()[\]]*\s)?(.*)\n`)

func (n *node) output(s string) {
	if n.lvl == 1 {
		matches := packageSummaryPattern.FindStringSubmatch(s)
		if len(matches) == 4 {
			n.msg = matches[3]
		}

		return
	}
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
