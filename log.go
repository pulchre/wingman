package wingman

import (
	"log"
	"os"
)

type Logger interface {
	Fatal(...interface{})
	Fatalf(string, ...interface{})
	Print(...interface{})
	Printf(string, ...interface{})
}

var Log Logger = log.New(os.Stderr, "", log.LstdFlags)
