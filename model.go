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
func (m *model) nodeFor(ev TestEvent) *node {
	nameParts := strings.Split(ev.Test, "/")
	if len(nameParts) == 1 && strings.TrimSpace(nameParts[0]) == "" {
		nameParts = []string{}
	}
	nameParts = append([]string{ev.Package}, nameParts...)

	var last, next *node

	for next = &m.root; len(nameParts) > 0 && next != nil; {
		last = next
		next, nameParts = last.findChild(nameParts)
	}

	if next != nil {
		return next
	}

	// node not found, so create a new node
	// we assume that we will *always* see a separate event
	// for a parent node before we see any event for a child node.
	// That means, if we got here and we're *not* on the last namePart,
	// then this event must represent a test that has a slash in the name.
	// So the name of this new node must be all the remaining name parts
	// joined back together with slashes.
	node := node{
		name:       strings.Join(nameParts, "/"),
		parent:     last,
		isTest:     ev.Test != "",
		start:      time.Now(),
		lvl:        last.lvl + 1,
		firstStart: time.Now(),
	}

	last.children = append(last.children, &node)

	return &node
}

func (m *model) processEvent(ev TestEvent) tea.Cmd {
	currNode := m.nodeFor(ev)

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
		// do a final sort of the children, and drop children which should be dropped
		currNode.children = processChildren(currNode.children, true)
	}

	// if node is finished, dump its output if appropriate
	if currNode.done && currNode.outputBuf != nil {
		if !drop(currNode) {
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
		// we can drop the output now to free up memory.
		currNode.outputBuf = nil
	}

	// re-sort and filter this node's siblings based on the status change
	parent := currNode.parent
	parent.children = processChildren(parent.children, false)

	return nil
}

func processChildren(s []*node, final bool) []*node {
	if len(s) == 0 {
		return s
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
	return s
}

func nodeSorter(final bool) func(*node, *node) int {
	return func(a, b *node) int {
		if final {
			// sort dropped nodes to the end
			if da, db := drop(a), drop(b); da != db {
				if da {
					return 1
				}
				return -1

			}
		}
		// sort done nodes to the top
		if a.done != b.done {
			if a.done {
				return -1
			} else {
				return 1
			}
		}
		if a.done {
			// sort done nodes by finished time ascending
			return a.doneTs.Compare(b.doneTs)
		}

		// sort running nodes by started time, ascending
		return a.firstStart.Compare(b.firstStart)
	}
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
	default:
		icon = "??? " + n.status + " ???"
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

	if max < 0 {
		max = 0
	}

	// first, collect of list of nodes which are candidates for eliding, sorted in order of
	// preferred eliding order: deepest first, then passed or skipped, paused, failed, then running
	candidates := make([]*list.Element, 0, l.Len())
	for e := range listSeq(l) {
		candidates = append(candidates, e)
	}

	slices.SortStableFunc(candidates, func(a, b *list.Element) int {
		an, bn := a.Value.(*node), b.Value.(*node)

		if i := statusRank(an.status) - statusRank(bn.status); i != 0 {
			return i
		}

		if i := bn.lvl - an.lvl; i != 0 {
			return i
		}

		if i := statusRank2(an.status) - statusRank2(bn.status); i != 0 {
			return i
		}

		if i := an.doneTs.Compare(bn.doneTs); i != 0 {
			return i
		}

		if i := an.firstStart.Compare(bn.firstStart); i != 0 {
			return i
		}

		return 0

	})

	// remove candidates until either we run out, or the list is short enough
	for i := 0; i < len(candidates) && l.Len() > max; i++ {
		// when we remove a node, also remove all its descendents
		// always call Next() *before* removing the current element,
		// calling Next() on an element that's already been removed just
		// returns nil
		e := candidates[i]
		for e1 := e.Next(); e1 != nil; {
			if e1.Value.(*node).lvl > e.Value.(*node).lvl {
				e2 := e1.Next()
				l.Remove(e1)
				e1 = e2
			} else {
				break
			}
		}

		l.Remove(e)
	}

	return l
}

func statusRank2(s string) int {
	switch s {
	case "pass":
		return 1
	case "pause":
		return 1
	case "skip":
		return 1
	case "fail":
		return 4
	case "cont", "start", "run", "bench":
		return 5
	}
	return 0
}

func statusRank(s string) int {
	switch s {
	case "pass":
		return 1
	case "pause":
		return 1
	case "skip":
		return 1
	case "fail":
		return 5
	case "cont", "start", "run", "bench":
		return 5
	}
	return 0
}

func collectNodes(nodes []*node) *list.List {
	l := list.New()

	if nodes == nil {
		return l
	}

	stack := make([][]*node, 0, 50)
	stack = append(stack, nodes)

	for i := len(stack) - 1; i >= 0; {
		s := stack[i]
		if len(s) == 0 {
			// pop off the stack
			stack = stack[:i]
			i--
			continue
		}

		// process the first node in the current slice
		n := s[0]
		l.PushBack(n)

		// dequeue the first node in the current slice
		stack[i] = s[1:]

		// if current node has children, push the children into the stack
		// and bump i to process the children next
		if len(n.children) > 0 {
			stack = append(stack, n.children)
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

	origLen := l.Len()

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
	fmt.Fprintf(&sb, " in %s", round(scaledTimeSince(m.start), 1))
	if flags.debug {
		fmt.Fprintf(&sb, " h: %v maxPrinted: %v origLen: %v printedLen: %v", m.windowHeight, m.maxPrintedLines, origLen, l.Len())
	}

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
	case len(n.children) > 0:
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
