package main

import (
	"bytes"
	"regexp"
	"strings"
	"time"
)

type node struct {
	name       string
	firstStart time.Time
	start      time.Time
	status     string
	done       bool
	doneTs     time.Time
	children   []*node
	parent     *node
	outputBuf  *bytes.Buffer
	elapsed    time.Duration
	isTest     bool
	lvl        int
	msg        string
}

var packageSummaryPattern = regexp.MustCompile(`^(.{4})?\t\S+\t([^\s()[\]]*)?(\t(.*))?\n`)

func (n *node) output(s string) {
	if n.lvl == 1 {
		matches := packageSummaryPattern.FindStringSubmatch(s)

		switch {
		case len(matches) == 5:
			// set node message, then skip
			n.msg = matches[4]
			return
		case s == "PASS\n":
			// skip
			return
		case s == "FAIL\n":
			// skip
			return
		case s == "SKIP\n":
			// skip
			return
		case strings.HasPrefix(s, "coverage: "):
			// skip
			return
		}

		n.append(s)

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
