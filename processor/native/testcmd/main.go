package main

import (
	"os"

	"github.com/pulchre/wingman/processor/native"
)

func main() {
	if os.Getenv("WINGMAN_NATIVE_SUBPROCESS") != "1" {
		panic("WINGMAN_NATIVE_SUBPROCESS=1 is not present in the environment")
	}

	p, err := native.NewSubprocess()
	if err != nil {
		panic(err)
	}

	err = p.Start()
	if err != nil {
		panic(err)
	}
}
