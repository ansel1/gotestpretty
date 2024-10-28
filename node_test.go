package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPattern(t *testing.T) {

}

func TestPackageMsg(t *testing.T) {
	testCases := map[string]string{
		"FAIL\tgitlab.protectv.local/ncryptify/sallyport.git/models\t5.430s\n":                                          "",
		"\tgitlab.protectv.local/ncryptify/sallyport.git/cmd/sallyport\t\tcoverage: 0.0% of statements\n":               "coverage: 0.0% of statements",
		"ok  \tgitlab.protectv.local/ncryptify/sallyport.git\t0.946s\tcoverage: 0.0% of statements [no tests to run]\n": "coverage: 0.0% of statements [no tests to run]",
	}
	for in, out := range testCases {
		n := &node{lvl: 1}

		n.output(in)
		assert.Equal(t, out, n.msg)
	}
}
