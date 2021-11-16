package main

import (
	"os"

	"github.com/google/uuid"
	"github.com/pulchre/wingman/processor/native"
)

func main() {
	if os.Getenv("WINGMAN_NATIVE_SUBPROCESS") != "1" {
		panic("WINGMAN_NATIVE_SUBPROCESS=1 is not present in the environment")
	}

	if os.Getenv("WINGMAN_NATIVE_SUBPROCESS_ID") == "" {
		panic("WINGMAN_NATIVE_SUBPROCESS_ID is not present in the environment")
	}

	id, err := uuid.Parse(os.Getenv("WINGMAN_NATIVE_SUBPROCESS_ID"))
	if err != nil {
		panic(err)
	}

	p := native.NewSubprocessor(id)
	err = p.StartSubprocess()
	if err != nil {
		panic(err)
	}
}
