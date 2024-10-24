package main

import (
	"bufio"
	"bytes"
	"container/list"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/v2/spinner"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	root                        node
	err                         error
	done                        bool
	spinner                     spinner.Model
	prog                        *tea.Program
	passes, fails, skips, total int
	start                       time.Time
	windowHeight                int
	maxPrintedLines             int
}

func newModel() *model {
	return &model{
		start:   time.Now(),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func (m *model) Init() (tea.Model, tea.Cmd) {
	return m, m.spinner.Tick
}

// nodeFor traverses the tree looking for the node that represents the object related
// to the event.  If nodes in the path doesn't exist yet, it is created.
func (m *model) nodeFor(e TestEvent) *node {
	nameParts := strings.Split(e.Test, "/")
	if len(nameParts) == 1 && strings.TrimSpace(nameParts[0]) == "" {
		nameParts = []string{}
	}
	nameParts = append([]string{e.Package}, nameParts...)

	findNode := func(name string, currNode *node) *node {
		for i, child := range currNode.children {
			if child.name == name {
				return &currNode.children[i]
			}
		}

		// node doesn't exist, create it
		node := node{
			name:   name,
			parent: currNode,
			isTest: e.Test != "",
			start:  time.Now(),
			lvl:    currNode.lvl + 1,
		}
		currNode.children = append(currNode.children, node)
		return &currNode.children[len(currNode.children)-1]
	}

	currNode := &m.root
	for _, s := range nameParts {
		currNode = findNode(s, currNode)
	}

	return currNode
}

func (m *model) processEvent(e TestEvent) tea.Cmd {
	currNode := m.nodeFor(e)

	if e.Elapsed > 0 {
		currNode.elapsed = time.Duration(e.Elapsed * float64(time.Second))
		currNode.start = time.Time{}
	}

	if e.Action == "output" {
		currNode.output(e.Output)
		// for output, return immediately.  not a node state.
		return nil
	}

	currNode.status = e.Action

	switch e.Action {
	case "fail":
		if currNode.isTest {
			m.fails++
			m.total++
		}
		currNode.done = true
	case "skip":
		if currNode.isTest {
			m.skips++
			m.total++
		}
		currNode.done = true
	case "pause":
		if currNode.isTest {
			currNode.elapsed = time.Since(currNode.start)
			currNode.start = time.Now()
		}
	case "cont":
		currNode.start = time.Now()
	case "start", "run", "bench":
	case "pass":
		if currNode.isTest {
			m.passes++
			m.total++
		}
		currNode.done = true
	}

	if currNode.done && !skip(currNode) && currNode.outputBuf != nil {
		// if this is a child test that has just finished, merge its
		// output into the output of its parent.
		if currNode.parent != nil && currNode.parent.isTest {
			if currNode.parent.outputBuf == nil {
				currNode.parent.outputBuf = bytes.NewBuffer(nil)
			}
			copyWithIndent(currNode.outputBuf, currNode.parent.outputBuf)
		} else {
			// this is a top-level test, all it's children are done,
			// so it is safe to dump this test's output to the console
			output := currNode.outputBuf.String()
			output = strings.TrimRight(output, "\n")
			return func() tea.Msg {
				m.prog.Println(output)
				return nil
			}
		}

	}

	return nil

}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowHeight = msg.Height
		m.maxPrintedLines = 0
	case error:
		m.err = msg
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.done = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case TestEvent:
		return m, m.processEvent(msg)
	case Done:
		m.done = true
		return m, m.printFinalSummary
	}
	return m, nil
}

func (m *model) printFinalSummary() tea.Msg {
	// reset the view
	m.maxPrintedLines = 0
	m.prog.Println(m.String())
	return tea.QuitMsg{}
}

// printNode prints a line to the writer representing this node, then recursive prints
// each of the child nodes.  Returns the total number of lines printed.
func (m *model) printNode(n *node, writer io.Writer) {
	for i := -1; i < n.lvl; i++ {
		_, _ = writer.Write([]byte("  "))
	}
	m.println(n, writer)
}

var iconPassed = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true).Render("✓")
var iconSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true).Render("️⍉")
var iconFailed = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true).Render("️✖")

func (m *model) println(n *node, writer io.Writer) {
	if n.name == "" {
		// root node, no self output
		return
	}

	// the min elapsed time.  If elapsed is less then this, the elapsed time will not be rendered
	var minElapsed time.Duration
	digits := 2

	if !n.done {
		// suppress elapsed times less than 1 second
		minElapsed = time.Second
		digits = 1
	}

	switch n.status {
	case "start", "run", "cont", "bench":
		fmt.Fprintf(writer, "%s %s %s", m.spinner.View(), n.name, formatElapsed(n.elapsed+scaledTimeSince(n.start), minElapsed, digits))
	case "pause":
		fmt.Fprintf(writer, "⏸ %s %s", n.name, formatElapsed(n.elapsed, minElapsed, digits))
	case "fail":
		fmt.Fprintf(writer, "%s %s %s", iconFailed, n.name, formatElapsed(n.elapsed, minElapsed, digits))
	case "skip":
		fmt.Fprintf(writer, "%s %s %s", iconSkipped, n.name, formatElapsed(n.elapsed, minElapsed, digits))
	case "pass":
		fmt.Fprintf(writer, "%s %s %s", iconPassed, n.name, formatElapsed(n.elapsed, minElapsed, digits))
	}

	fmt.Fprint(writer, "\n")
}

func (m *model) View() string {
	if m.err != nil {
		return m.err.Error()
	}
	// once we're done, we don't want to print any view.  The final
	// summary will be dumped to the terminal with tea.Program#Println()
	if m.done {
		return ""
	}

	return m.render(true)
}

func elide(l *list.List, max int) *list.List {
	if l.Len() <= max {
		return l
	}

	// first, collect of list of nodes which are candidates for eliding, sorted in order of
	// preferred eliding order: deepest first, then passed or skipped, paused, failed, then running

	candidates := make([]*list.Element, 0, l.Len())
	for e := l.Front(); e != nil; e = e.Next() {
		if e.Value.(*node).lvl > 1 {
			candidates = append(candidates, e)
		}
	}

	slices.SortStableFunc(candidates, func(a, b *list.Element) int {
		an, bn := a.Value.(*node), b.Value.(*node)
		if i := bn.lvl - an.lvl; i != 0 {
			return i
		}

		if an.done != bn.done {
			if an.done {
				return -1
			} else {
				return 1
			}
		}

		if an.status == bn.status {
			return 0
		}

		return statusRank(an.status) - statusRank(bn.status)
	})

	toRm := l.Len() - max
	if toRm > len(candidates) {
		toRm = len(candidates)
	}

	for _, e := range candidates[:toRm] {
		l.Remove(e)
	}

	return l
}

func statusRank(s string) int {
	switch s {
	case "pass", "skip":
		return 1
	case "fail":
		return 3
	case "pause":
		return 2
	case "cont", "start", "run", "bench":
		return 4
	}
	return 0
}

// returns a flat list of all printable nodes, and the max lvl seen
func collChildren(nodes []node) *list.List {
	if len(nodes) == 0 {
		return nil
	}

	l := list.New()

	var lastDone *list.Element

	for i := range nodes {
		c := &nodes[i]
		if skip(c) {
			continue
		}

		var e *list.Element

		// sort finished child nodes first
		if c.done {
			if lastDone == nil {
				e = l.PushFront(c)
			} else {
				e = l.InsertAfter(c, lastDone)
			}
		} else {
			e = l.PushBack(c)
		}

		cl := collChildren(c.children)
		if cl != nil && cl.Len() > 0 {
			for child := cl.Front(); child != nil; child = child.Next() {
				e = l.InsertAfter(child.Value, e)
			}
		}

		if c.done {
			lastDone = e
		}
	}

	return l
}

func (m *model) String() string {
	return m.render(false)
}

func (m *model) render(fitToWindow bool) string {
	var sb strings.Builder

	l := collChildren(m.root.children)

	if l == nil || l.Len() == 0 {
		// if no tests have started yet, don't print anything
		return ""
	}

	if fitToWindow {
		l = elide(l, m.windowHeight-2)
	}

	for e := l.Front(); e != nil; e = e.Next() {
		m.printNode(e.Value.(*node), &sb)
	}

	if fitToWindow {
		printedLines := l.Len() + 2

		if printedLines >= m.maxPrintedLines {
			m.maxPrintedLines = printedLines
		} else {
			// add extra empty lines so the summary stays pinned to the bottom
			for i := m.maxPrintedLines - printedLines; i > 0; i-- {
				fmt.Fprintf(&sb, "\n")
			}
		}
	}

	fmt.Fprintf(&sb, "\n")
	if m.done {
		sb.WriteString("DONE ")
	}

	fmt.Fprintf(&sb, "%d tests", m.total)
	if m.skips > 0 {
		fmt.Fprintf(&sb, ", %d skipped", m.skips)
	}
	if m.fails > 0 {
		fmt.Fprintf(&sb, ", %d failed", m.fails)
	}
	fmt.Fprintf(&sb, " in %s\n", round(scaledTimeSince(m.start), 1))

	return sb.String()
}

func scaledTimeSince(t time.Time) time.Duration {
	s := time.Since(t)
	if flags.replay && flags.rate > 0 {
		s = time.Duration(float64(s) / flags.rate)

	}
	return s
}

// func printBufBytes(buf *bytes.Buffer) string {
// 	if buf == nil {
// 		return ""
// 	}

// 	return byteCountDecimal(buf.Len())
// }

// func byteCountDecimal(b int) string {
// 	const unit = 1000
// 	if b < unit {
// 		return fmt.Sprintf("%dB", b)
// 	}
// 	div, exp := int64(unit), 0
// 	for n := b / unit; n >= unit; n /= unit {
// 		div *= unit
// 		exp++
// 	}

// 	return fmt.Sprintf("%.9g%cB", float64(b)/float64(div), "kMGTPE"[exp])
// }

func formatElapsed(d, min time.Duration, digits int) string {
	if d < min {
		return ""
	}

	return round(d, digits).String()
}

var divs = []time.Duration{
	time.Duration(1), time.Duration(10), time.Duration(100), time.Duration(1000)}

func round(d time.Duration, digits int) time.Duration {
	switch {
	case d > time.Second:
		d = d.Round(time.Second / divs[digits])
	case d > time.Millisecond:
		d = d.Round(time.Millisecond / divs[digits])
	case d > time.Microsecond:
		d = d.Round(time.Microsecond / divs[digits])
	}
	return d
}

func copyWithIndent(from, to *bytes.Buffer) {
	s := bufio.NewScanner(from)
	for s.Scan() {
		to.WriteString("    ")
		to.WriteString(s.Text())
		to.WriteString("\n")
	}
}

func skip(n *node) bool {
	switch {
	case !n.isTest:
		return false
	case flags.includeSlow && n.elapsed > flags.slowThreshold:
		return false
	case !flags.includeSkipped && n.status == "skip":
		return true
	case !flags.includePassed && n.status == "pass":
		return true
	}
	return false
}
