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
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var flags struct {
	replay         bool
	rate           float64
	infile         string
	includePassed  bool
	includeSkipped bool
	includeSlow    bool
	slowThreshold  time.Duration
}

func init() {
	flag.BoolVar(&flags.replay, "replay", false, "Replay events with pauses to simulate original test run")
	flag.Float64Var(&flags.rate, "rate", 1, "Set rate to replay, defaults to 1 (original speed), 0.5 = double speed, 0 = no pauses, ignored unless --replay=true")
	flag.StringVar(&flags.infile, "f", "", "Read from <filename> instead of stdin")
	flag.BoolVar(&flags.includePassed, "include-passed", false, "Include passed tests")
	flag.BoolVar(&flags.includeSlow, "include-slow", false, "Include slow tests")
	flag.BoolVar(&flags.includeSkipped, "include-skipped", true, "Include skipped tests")
	flag.DurationVar(&flags.slowThreshold, "slow-threshold", time.Second, "Set slow threshold")

	flag.Parse()
}

func main() {

	m := initialModel()
	p := tea.NewProgram(m, tea.WithInput(nil))

	m.prog = p

	if _, err := p.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if m.fails > 0 {
		os.Exit(1)
	}
}

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

type model struct {
	root                        node
	err                         error
	done                        bool
	spinner                     spinner.Model
	prog                        *tea.Program
	passes, fails, skips, total int
	start                       time.Time
}

func initialModel() *model {
	return &model{
		start:   time.Now(),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func (m *model) process(r io.Reader) tea.Msg {
	var lastTs time.Time

	s := bufio.NewScanner(r)
	for s.Scan() {
		var e TestEvent
		decoder := json.NewDecoder(bytes.NewReader(s.Bytes()))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&e)
		if err != nil {
			// this line wasn't valid json, so just print it
			m.prog.Println(s.Text())
			continue
		}

		if flags.replay {
			if !lastTs.IsZero() && !e.Time.IsZero() {
				pause := e.Time.Sub(lastTs)
				pause = time.Duration(float64(pause) * flags.rate)
				time.Sleep(pause)
			}
			lastTs = e.Time
		}

		m.prog.Send(e)
	}
	return Done{}
}

func (m *model) Init() tea.Cmd {
	f := func() tea.Msg {
		var r io.Reader
		if flags.infile != "" {
			f, err := os.Open(flags.infile)
			if err != nil {
				return err
			}
			r = f
		} else {
			r = os.Stdin
		}
		return m.process(bufio.NewReader(r))
	}
	return tea.Batch(m.spinner.Tick, f)
}

func (m *model) nodeFor(e TestEvent) *node {
	nameParts := strings.Split(e.Test, "/")
	if len(nameParts) == 1 && strings.TrimSpace(nameParts[0]) == "" {
		nameParts = []string{}
	}
	nameParts = append([]string{e.Package}, nameParts...)
	currNode := &m.root
	var currNodeIdx int

	findNode := func(name string) {
		for i, child := range currNode.children {
			if child.name == name {
				currNode = &currNode.children[i]
				currNodeIdx = i

				return
			}
		}

		node := node{
			name:   name,
			parent: currNode,
			isTest: e.Test != "",
			start:  time.Now(),
		}
		currNode.children = append(currNode.children, node)
		currNodeIdx = len(currNode.children) - 1
		currNode = &currNode.children[currNodeIdx]
	}

	for _, s := range nameParts {
		findNode(s)
	}

	return currNode

}

func (m *model) processEvent(e TestEvent) {
	currNode := m.nodeFor(e)

	if e.Output != "" {
		if currNode.outputBuf == nil {
			currNode.outputBuf = bytes.NewBufferString(e.Output)
		} else {
			currNode.outputBuf.WriteString(e.Output)
		}
	}

	if e.Elapsed > 0 {
		currNode.elapsed = time.Duration(e.Elapsed * float64(time.Second))
		currNode.start = time.Time{}
	}

	switch e.Action {
	case "fail":
		if currNode.isTest {
			m.fails++
			m.total++
		}
		currNode.status = e.Action
	case "skip":
		if currNode.isTest {
			m.skips++
			m.total++
		}
		currNode.status = e.Action
	case "pause":
		currNode.status = e.Action
		if currNode.isTest {
			currNode.elapsed = time.Since(currNode.start)
			currNode.start = time.Now()
		}
	case "cont":
		currNode.status = e.Action
		currNode.start = time.Now()
	case "start", "run", "bench":
		currNode.status = e.Action
	case "pass":
		if currNode.isTest {
			m.passes++
			m.total++
		}
		currNode.status = e.Action
	}

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
		m.processEvent(msg)
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

func scaledTimeSince(t time.Time) time.Duration {
	s := time.Since(t)
	if flags.replay && flags.rate > 0 {
		s = time.Duration(float64(s) / flags.rate)

	}
	return s
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

func printBufBytes(buf *bytes.Buffer) string {
	if buf == nil {
		return ""
	}

	return byteCountDecimal(buf.Len())
}

func byteCountDecimal(b int) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
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
