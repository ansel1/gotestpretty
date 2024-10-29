package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPackageMsg(t *testing.T) {
	// all the test lines are in a single text block.  The test will split this into lines, and compare each line with
	// the corresponding index in `out`.
	// To add more test cases, add another line, terminated with a newline, to `in`, and a new expectation to `out`.
	//
	// This is one text block to make it easy to copy all the samples into a regex tool to test them all against
	// the pattern at once.

	in := `FAIL	gitlab.protectv.local/ncryptify/sallyport.git/models	5.430s
	gitlab.protectv.local/ncryptify/sallyport.git/cmd/sallyport		coverage: 0.0% of statements
ok  	gitlab.protectv.local/ncryptify/sallyport.git	0.946s	coverage: 0.0% of statements [no tests to run]
?   	gitlab.protectv.local/ncryptify/minerva.git/cryptocore	[no test files]
`
	out := []string{
		"",
		"coverage: 0.0% of statements",
		"coverage: 0.0% of statements [no tests to run]",
		"[no test files]",
	}

	for i, line := range strings.SplitAfter(in, "\n") {
		if i+1 > len(out) {
			return
		}
		n := &node{lvl: 1}

		n.output(line)
		assert.Equal(t, out[i], n.msg, "incorrect output for line %v: %v", i, line)
	}
}
