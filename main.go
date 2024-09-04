package main

// A simple program demonstrating the spinner component from the Bubbles
// component library.

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
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

type testNode struct {
	name      string
	output    string
	status    string
	children  []testNode
	parent    *testNode
	outputBuf *bytes.Buffer
	elapsed   time.Duration
}

type model struct {
	rootNode testNode
	err      error
	quitting bool
	spinner  spinner.Model
	onEvent  func(TestEvent)
}

func initialModel() *model {
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return &model{
		spinner: spin,
	}
}

func (m *model) Init() tea.Cmd {
	f := func() tea.Msg {
		p := NewProcessor(bufio.NewReader(os.Stdin))
		p.onEvent = m.onEvent
		return p.Process()
	}
	return tea.Batch(m.spinner.Tick, tea.Sequence(f, tea.Quit))
}

func (m *model) processEvent(e TestEvent) {
	split := strings.Split(e.Test, "/")
	if len(split) == 1 && strings.TrimSpace(split[0]) == "" {
		split = []string{}
	}
	split = append([]string{e.Package}, split...)
	currNode := &m.rootNode
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
		node := testNode{
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
	case "start", "run", "pause", "cont", "bench", "fail", "skip":
		currNode.output = ""
		currNode.status = e.Action
	case "pass":
		currNode.status = e.Action
		if currNode.parent != nil && currNode.parent.parent != nil {
			parent := currNode.parent
			parent.children = slices.Delete(parent.children, currNodeIdx, currNodeIdx+1)
			// don't ref currNode again, it will be zeroized by the above call
		}
	case "output":
		o := e.Output
		o = strings.Replace(o, "\n", "\\n", -1)
		currNode.output = o
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

func (m *model) printNode(n *testNode, lvl int, writer io.Writer) {
	for i := 0; i < lvl; i++ {
		writer.Write([]byte("  "))
	}
	m.println(n, writer)
	for i := range n.children {
		m.printNode(&n.children[i], lvl+1, writer)
	}
}

func (m *model) println(n *testNode, writer io.Writer) {
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
		fmt.Fprintf(writer, "✖ %s %s %s\n", n.name, printBufBytes(n.outputBuf), n.elapsed.String())
	case "skip":
		fmt.Fprintf(writer, "⍉️ %s %s %s\n", n.name, printBufBytes(n.outputBuf), n.elapsed.String())
	case "pass":
		fmt.Fprintf(writer, "✓ %s %s %s\n", n.name, printBufBytes(n.outputBuf), n.elapsed.String())
	}

}

func (m *model) View() string {
	if m.err != nil {
		return m.err.Error()
	}

	var sb strings.Builder
	m.printNode(&m.rootNode, 0, &sb)

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
