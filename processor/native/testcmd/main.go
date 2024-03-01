package main

import (
	"fmt"
	"os"

	_ "github.com/pulchre/wingman/mock"
	"github.com/pulchre/wingman/processor/native"
)

func main() {
	test := os.Getenv("WINGMAN_TEST")
	if os.Getenv(native.ClientEnvironmentName) != "1" {
		panic(fmt.Sprintf("test=%s %s=1 is not present in the environment", test, native.ClientEnvironmentName))
	}

	c, err := native.NewClient()
	if err != nil {
		panic(fmt.Sprintf("NewClient: test=%s err=%v", test, err))
	}

	err = c.Start()
	if err != nil {
		panic(fmt.Sprintf("Start: test=%s err=%v", test, err))
	}
}
