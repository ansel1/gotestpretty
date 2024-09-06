package main

import (
	"flag"
	"fmt"
	"strconv"
)

type replayRate float64

func (r *replayRate) Set(value string) error {
	if value == "" {
		*r = replayRate(1)
	}

	float, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}

	*r = replayRate(float)
	return nil
}

func (r *replayRate) String() string {
	return fmt.Sprintf("%f", *r)
}

var rate replayRate

func main() {
	flag.Var(&rate, "replay", "")

	flag.Parse()

	fmt.Println(rate)

}
