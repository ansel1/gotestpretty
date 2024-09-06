package main

// A simple program demonstrating the spinner component from the Bubbles
// component library.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ansel1/merry/v2"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

var flags struct {
	replay         bool
	rate           float64
	infile         string
	includePassed  bool
	includeSkipped bool
	includeSlow    bool
}

func init() {
	flag.BoolVar(&flags.replay, "replay", false, "Replay events with pauses to simulate original test run")
	flag.Float64Var(&flags.rate, "rate", 1, "Set rate to replay, defaults to 1 (original speed), 0.5 = double speed, 0 = no pauses, ignored unless --replay=true")
	flag.StringVar(&flags.infile, "f", "", "Read from <filename> instead of stdin")
	flag.BoolVar(&flags.includePassed, "include-passed", false, "Include passed tests")
	flag.BoolVar(&flags.includeSlow, "include-slow", false, "Include slow tests")
	flag.BoolVar(&flags.includeSkipped, "include-skipped", true, "Include skipped tests")
	flag.Parse()
}

func main() {

	m := initialModel()
	p := tea.NewProgram(m, tea.WithInput(nil))

	var lastTs time.Time
	m.onEvent = func(e TestEvent) {
		if flags.replay {
			if !lastTs.IsZero() && !e.Time.IsZero() {
				pause := e.Time.Sub(lastTs)
				pause = time.Duration(float64(pause) * flags.rate)
				time.Sleep(pause)
			}
			lastTs = e.Time
		}
		p.Send(e)
	}

	if _, err := p.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type node struct {
	name      string
	status    string
	children  []node
	parent    *node
	outputBuf *bytes.Buffer
	elapsed   time.Duration
}

type model struct {
	root                        node
	err                         error
	quitting                    bool
	spinner                     spinner.Model
	onEvent                     func(TestEvent)
	passes, fails, skips, total int
	start                       time.Time
}

func initialModel() *model {
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return &model{
		start:   time.Now(),
		spinner: spin,
	}
}

func process(r io.Reader, onEvent func(TestEvent)) error {
	s := bufio.NewScanner(r)
	for s.Scan() {
		var e TestEvent
		err := json.Unmarshal(s.Bytes(), &e)
		if err != nil {
			return merry.Prepend(err, "failed to parse test event")
		}
		e.orig = s.Text()

		onEvent(e)
	}
	return nil
}

func (m *model) Init() tea.Cmd {
	f := func() tea.Msg {
		return process(bufio.NewReader(os.Stdin), m.onEvent)
	}
	return tea.Batch(m.spinner.Tick, tea.Sequence(f, tea.Quit))
}

func (m *model) processEvent(e TestEvent) {
	split := strings.Split(e.Test, "/")
	if len(split) == 1 && strings.TrimSpace(split[0]) == "" {
		split = []string{}
	}
	split = append([]string{e.Package}, split...)
	currNode := &m.root
	var currNodeIdx int

TOP:
	for _, s := range split {
		for i, child := range currNode.children {
			if child.name == s {
				currNode = &currNode.children[i]
				currNodeIdx = i
				continue TOP
			}
		}
		node := node{
			name:   s,
			parent: currNode,
		}
		currNode.children = append(currNode.children, node)
		currNodeIdx = len(currNode.children) - 1
		currNode = &currNode.children[currNodeIdx]
	}

	if e.Output != "" {
		if currNode.outputBuf == nil {
			currNode.outputBuf = bytes.NewBufferString(e.Output)
		} else {
			currNode.outputBuf.WriteString(e.Output)
		}
	}

	if e.Elapsed > 0 {
		currNode.elapsed = time.Duration(e.Elapsed * float64(time.Second))
	}

	switch e.Action {
	case "fail":
		m.fails++
		m.total++
		currNode.status = e.Action
	case "skip":
		m.skips++
		m.total++
		currNode.status = e.Action
	case "start", "run", "pause", "cont", "bench":
		currNode.status = e.Action
	case "pass":
		m.passes++
		m.total++
		currNode.status = e.Action
		if currNode.parent != nil && currNode.parent.parent != nil {
			parent := currNode.parent
			parent.children = slices.Delete(parent.children, currNodeIdx, currNodeIdx+1)
			// don't ref currNode again, it will be zeroized by the above call
		}
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
			m.quitting = true
			return m, tea.Quit
		default:
			return m, nil
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case TestEvent:
		m.processEvent(msg)
		return m, nil
	}
	return m, nil
}

func (m *model) printNode(n *node, lvl int, writer io.Writer) {
	for i := 0; i < lvl; i++ {
		writer.Write([]byte("  "))
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
		fmt.Fprintf(writer, "%s %s %s\n", m.spinner.View(), n.name, printBufBytes(n.outputBuf))
	case "pause":
		fmt.Fprintf(writer, "⏸ %s %s\n", n.name, printBufBytes(n.outputBuf))
	case "fail":
		fmt.Fprintf(writer, "%s %s %s %s\n", iconFailed, n.name, printBufBytes(n.outputBuf), n.elapsed.String())
	case "skip":
		fmt.Fprintf(writer, "%s %s %s %s\n", iconSkipped, n.name, printBufBytes(n.outputBuf), n.elapsed.String())
	case "pass":
		fmt.Fprintf(writer, "%s %s %s %s\n", iconPassed, n.name, printBufBytes(n.outputBuf), n.elapsed.String())
	}

}

func (m *model) View() string {
	if m.err != nil {
		return m.err.Error()
	}

	var sb strings.Builder
	m.printNode(&m.root, 0, &sb)

	fmt.Fprintf(&sb, "\n%d tests", m.total)
	if m.skips > 0 {
		fmt.Fprintf(&sb, ", %d skipped", m.skips)
	}
	if m.fails > 0 {
		fmt.Fprintf(&sb, ", %d failed", m.fails)
	}
	fmt.Fprintf(&sb, " in %s\n", time.Now().Sub(m.start).String())

	if m.quitting {
		sb.WriteRune('\n')
	}
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
