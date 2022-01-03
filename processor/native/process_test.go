package native

import (
	"fmt"
	"testing"

	"github.com/pulchre/wingman/mock"
)

const notFoundError = `exec: "DOESNOTEXIST": executable file not found in $PATH`

func TestProcessStart(t *testing.T) {
	mock.TestLog.Reset()

	table := []struct {
		in  PoolOptions
		out error
		bin string
	}{
		{DefaultOptions().SetProcesses(0).SetConcurrency(1), nil, ""},
		{DefaultOptions().SetProcesses(1).SetConcurrency(1), nil, ""},
		{DefaultOptions().SetProcesses(1).SetConcurrency(2), nil, ""},
		{DefaultOptions().SetProcesses(1).SetConcurrency(0), fmt.Errorf(notFoundError), "DOESNOTEXIST"},
	}

	for _, c := range table {
		var oldbin string

		p := newProcess(c.in)

		if c.bin != "" {
			oldbin = GetBinPath()
			SetBinPath(c.bin)
		}

		err := p.Start()
		CheckError(c.out, err, t)

		if err != nil {
			continue
		}

		if !mock.TestLog.PrintReceived("Starting subprocess pid=%d", p.cmd.Process.Pid) {
			t.Error("expected correct process id to be logged. Got: ", mock.TestLog.PrintVars)
		}

		if len(p.handles) != p.opts.Concurrency {
			t.Errorf("Expected %d handles Got %d", p.opts.Concurrency, len(p.handles))
		}

		err = p.Kill()
		if err != nil {
			t.Fatal("Expected nil error on kill. Got: ", err)
		}

		if c.bin != "" {
			SetBinPath(oldbin)
		}
	}
}
