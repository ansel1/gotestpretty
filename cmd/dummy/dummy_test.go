package main

import (
	"testing"
)

func TestSuccess(t *testing.T) {

}

func TestFail(t *testing.T) {
	t.Fail()
}

func TestSkipped(t *testing.T) {
	t.Skip("skipped")
}

// func TestError(t *testing.T) {
// 	panic(errors.New("error"))
// }
