package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
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
}

func newModel() *model {
	return &model{
		start:   time.Now(),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func (m *model) Init() tea.Cmd {
	return m.spinner.Tick
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

	var nodeFinished bool

	currNode.status = e.Action

	switch e.Action {
	case "fail":
		if currNode.isTest {
			m.fails++
			m.total++
		}
		nodeFinished = true
	case "skip":
		if currNode.isTest {
			m.skips++
			m.total++
		}
		nodeFinished = true
	case "pause":
		if currNode.isTest {
			currNode.elapsed = time.Since(currNode.start)
			currNode.start = time.Now()
		}
	case "cont":
		currNode.start = time.Now()
	// case "start", "run", "bench":
	case "pass":
		if currNode.isTest {
			m.passes++
			m.total++
		}
		nodeFinished = true
	}

	if nodeFinished && !skip(currNode) && currNode.outputBuf != nil {
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
	m.prog.Println(m.String())
	return tea.QuitMsg{}
}

func (m *model) printNode(n *node, lvl int, writer io.Writer) {
	if skip(n) {
		return
	}
	for i := 0; i < lvl; i++ {
		_, _ = writer.Write([]byte("  "))
	}
	m.println(n, writer)
	for i := range n.children {
		m.printNode(&n.children[i], lvl+1, writer)
	}
}

var iconPassed = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true).Render("✓")
var iconSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true).Render("️⍉")
var iconFailed = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true).Render("️✖")

func (m *model) println(n *node, writer io.Writer) {
	if n.name == "" {
		// root node, no self output
		return
	}

	switch n.status {
	case "start", "run", "cont", "bench":
		fmt.Fprintf(writer, "%s %s %s %s\n", m.spinner.View(), n.name, printBufBytes(n.outputBuf), round(n.elapsed+scaledTimeSince(n.start), 1))
	case "pause":
		fmt.Fprintf(writer, "⏸ %s %s %s\n", n.name, printBufBytes(n.outputBuf), round(n.elapsed, 1))
	case "fail":
		fmt.Fprintf(writer, "%s %s %s %s\n", iconFailed, n.name, printBufBytes(n.outputBuf), round(n.elapsed, 1))
	case "skip":
		fmt.Fprintf(writer, "%s %s %s %s\n", iconSkipped, n.name, printBufBytes(n.outputBuf), round(n.elapsed, 1))
	case "pass":
		fmt.Fprintf(writer, "%s %s %s %s\n", iconPassed, n.name, printBufBytes(n.outputBuf), round(n.elapsed, 1))
	}

}

func (m *model) View() string {
	// once we're done, we don't want to print any view.  The final
	// summary will be dumped to the terminal with tea.Program#Println()
	if m.done {
		return ""
	}

	return m.String()
}

func (m *model) String() string {
	if m.err != nil {
		return m.err.Error()
	}

	var sb strings.Builder

	m.printNode(&m.root, -1, &sb)

	fmt.Fprintf(&sb, "\n")
	if m.done {
		fmt.Fprintf(&sb, "DONE ")
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

func printBufBytes(buf *bytes.Buffer) string {
	if buf == nil {
		return ""
	}

	return byteCountDecimal(buf.Len())
}

func byteCountDecimal(b int) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.9g%cB", float64(b)/float64(div), "kMGTPE"[exp])
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
