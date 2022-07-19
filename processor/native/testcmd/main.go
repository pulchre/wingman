package main

import (
	"fmt"
	"os"

	_ "github.com/pulchre/wingman/mock"
	"github.com/pulchre/wingman/processor/native"
)

func main() {
	if os.Getenv(native.ClientEnvironmentName) != "1" {
		panic(fmt.Sprintf("%s=1 is not present in the environment", native.ClientEnvironmentName))
	}

	c, err := native.NewClient()
	if err != nil {
		panic(err)
	}

	err = c.Start()
	if err != nil {
		panic(err)
	}
}
