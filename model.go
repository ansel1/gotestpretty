package main

import (
	"bufio"
	"bytes"
	"container/list"
	"fmt"
	"io"
	"iter"
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

// listSeq is a helper method that lets you range over a linked list.
func listSeq(l *list.List) iter.Seq2[*list.Element, *node] {
	return func(yield func(*list.Element, *node) bool) {
		if l == nil {
			return
		}

		for e := l.Front(); e != nil; e = e.Next() {
			if !yield(e, e.Value.(*node)) {
				return
			}
		}
	}
}

// nodeFor traverses the tree looking for the node that represents the object related
// to the event.  If nodes in the path doesn't exist yet, it is created.
func (m *model) nodeFor(ev TestEvent) (*node, *list.Element) {
	nameParts := strings.Split(ev.Test, "/")
	if len(nameParts) == 1 && strings.TrimSpace(nameParts[0]) == "" {
		nameParts = []string{}
	}
	nameParts = append([]string{ev.Package}, nameParts...)

	findNode := func(name string, currNode *node) (*node, *list.Element) {
		for e, n := range listSeq(currNode.children) {
			if n.name == name {
				return n, e
			}
		}

		// node doesn't exist, create it
		node := node{
			name:       name,
			parent:     currNode,
			isTest:     ev.Test != "",
			start:      time.Now(),
			lvl:        currNode.lvl + 1,
			firstStart: time.Now(),
		}
		if currNode.children == nil {
			currNode.children = list.New()
		}

		return &node, currNode.children.PushBack(&node)
	}

	currNode := &m.root
	var e *list.Element

	for _, s := range nameParts {
		currNode, e = findNode(s, currNode)
	}

	return currNode, e
}

func (m *model) processEvent(ev TestEvent) tea.Cmd {
	currNode, e := m.nodeFor(ev)

	if ev.Elapsed > 0 {
		currNode.elapsed = time.Duration(ev.Elapsed * float64(time.Second))
		currNode.start = time.Time{}
	}

	if ev.Action == "output" {
		currNode.output(ev.Output)
		// for output, return immediately.  not a node state.
		return nil
	}

	currNode.status = ev.Action

	switch ev.Action {
	case "fail":
		if currNode.isTest {
			m.fails++
			m.total++
		}
		currNode.done = true
		currNode.doneTs = time.Now()
	case "skip":
		if currNode.isTest {
			m.skips++
			m.total++
		}
		currNode.done = true
		currNode.doneTs = time.Now()
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
		currNode.doneTs = time.Now()
	}

	if currNode.done {
		if drop(currNode) {
			// discard the node
			currNode.parent.children.Remove(e)
			return nil
		}

		if currNode.outputBuf != nil {
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

			// we can drop the output now that it has been printed.
			currNode.outputBuf = nil
		}
	}

	currNode.parent.children = sortChildren(currNode.parent.children)

	return nil
}

func sortChildren(l *list.List) *list.List {
	// this is inefficient, just testing out the idea first.

	s := make([]*node, 0, l.Len())

	for _, n := range listSeq(l) {
		s = append(s, n)
	}

	slices.SortStableFunc(s, func(a, b *node) int {
		if a.done != b.done {
			if a.done {
				return -1
			} else {
				return 1
			}
		}
		if a.done {
			// if both are done, sort by finished time, ascending
			return a.doneTs.Compare(b.doneTs)
		}

		// if both are still running, sort by started time, ascending
		return a.firstStart.Compare(b.firstStart)
	})

	l = list.New()
	for _, n := range s {
		l.PushBack(n)
	}
	return l

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
	for i := 1; i < n.lvl; i++ {
		_, _ = writer.Write([]byte("  "))
	}
	m.println(n, writer)
}

var iconPassed = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true).Render("✓")
var iconSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true).Render("️⍉")
var iconFailed = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true).Render("️✖")
var gray = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

func (m *model) println(n *node, writer io.Writer) {
	if n.name == "" {
		// root node, no self output
		return
	}

	elapsed := n.elapsed

	var icon string

	switch n.status {
	case "start", "run", "cont", "bench":
		elapsed = n.elapsed + scaledTimeSince(n.start)
		icon = m.spinner.View()
	case "pause":
		icon = "⏸"
	case "fail":
		icon = iconFailed
	case "skip":
		icon = iconSkipped
	case "pass":
		icon = iconPassed
	}

	// the min elapsed time.  If elapsed is less then this, the elapsed time will not be rendered
	var minElapsed time.Duration
	digits := 3

	if !n.done {
		// suppress elapsed times less than 1 second
		minElapsed = time.Second
		digits = 1
	}

	fmt.Fprintf(writer, "%s %s\t%s\t%s\n", icon, n.name, formatElapsed(elapsed, minElapsed, digits), gray.Render(n.msg))
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

func collectNodes(nodes *list.List) *list.List {
	l := list.New()

	if nodes == nil {
		return l
	}

	stack := make([]*list.Element, 0, 50)
	stack = append(stack, nodes.Front())

	for i := len(stack) - 1; i >= 0; {
		e := stack[i]
		if e == nil {
			// pop off the stack
			stack = stack[:i]
			i--
			continue
		}

		// process the current element
		l.PushBack(e.Value)

		// replace current element in the stack when the next sibling element
		stack[i] = e.Next()

		// if current element has children, push the first child onto the end of stack
		// and bump i to process the children next
		if cl := e.Value.(*node).children; cl != nil && cl.Len() > 0 {
			e1 := cl.Front()
			stack = append(stack, e1)
			i++
		}
	}

	return l
}

func (m *model) String() string {
	return m.render(false)
}

func (m *model) render(fitToWindow bool) string {
	var sb strings.Builder

	l := collectNodes(m.root.children)

	if l == nil || l.Len() == 0 {
		// if no tests have started yet, don't print anything
		return ""
	}

	if fitToWindow {
		l = elide(l, m.windowHeight-2)
	}

	for _, n := range listSeq(l) {
		m.printNode(n, &sb)
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

func drop(n *node) bool {
	switch {
	case !n.isTest:
		return false
	case flags.includeSlow && n.elapsed > flags.slowThreshold:
		return false
	case n.children != nil && n.children.Len() > 0:
		// don't drop the node if it still has any children
		// this only behaves correctly on the assumption that drop() is only called on the parent node
		// when all the child nodes have finished.
		return false
	case !flags.includeSkipped && n.status == "skip":
		return true
	case !flags.includePassed && n.status == "pass":
		return true
	}
	return false
}
