package main

import (
	"bytes"
	"regexp"
	"slices"
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

var packageSummaryPattern = regexp.MustCompile(`^(.{4})?\t\S+(\t[msh\d\.]*)?(\s(.*))?\n`)

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

// findChild searches for a child node.  It first looks for a child named
// nameParts[0], then if not found, looks for nameParts[0] + "/" + nameParts[1], etc.
// It keeps searching until it finds a matching child, or runs out of name parts.
// If a child is found, it returns the child, and the remaining unused name parts.
// If no child is found, returns nil, and all nameParts
func (n *node) findChild(nameParts []string) (*node, []string) {
	// Test names may have slashes in them, so we can't rely on simply splitting the test
	// name by slashes.  We need to see if there are any child nodes named after any compination of the remaining
	// name parts.
	for j := 1; j <= len(nameParts); j++ {
		name := strings.Join(nameParts[:j], "/")
		for _, c := range n.children {
			if c.name == name {
				return c, nameParts[j:]
			}
		}
	}
	return nil, nameParts
}

func (n *node) processChildren(final, recurse bool) {
	s := n.children
	if len(s) == 0 {
		return
	}

	if recurse {
		for _, c := range s {
			c.processChildren(final, recurse)
		}
	}

	slices.SortStableFunc(s, nodeSorter(final))

	if final {
		// droppable nodes should have been sorted to the end.
		for i := len(s) - 1; i >= 0; i-- {
			if drop(s[i]) {
				s[i].msg = "dropped" // debugging, should never be seen, if it is, something is wrong
				s[i] = nil           // blank ref to ensure gc
				s = s[:i]
			}
		}
	}

	n.children = s
}
