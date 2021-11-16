package native_test

import (
	"os"
	"syscall"
	"testing"

	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/processor/native"
)

const notFoundError = `exec: "DOESNOTEXIST": executable file not found in $PATH`

func TestStart(t *testing.T) {
	t.Run("Success", testStartSuccess)
	t.Run("BinaryDoesNotExist", testStartBinaryDoesNotExist)
}

func testStartSuccess(t *testing.T) {
	p := wingman.NewProcessor()
	err := p.Start()
	if err != nil {
		t.Errorf("processor.Start() got %v, expected nil", err)
	}
	defer killProcess(p)

	pid := p.(*native.Processor).Pid()
	if pid == -1 {
		t.Error("p.Pid() got -1 expect valid pid")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		t.Error("Failed to start process")
	} else if process.Signal(syscall.Signal(0)) != nil {
		t.Error("Failed to start process")
	}

}

func testStartBinaryDoesNotExist(t *testing.T) {
	oldBin := native.GetBinPath()
	defer func() {
		native.SetBinPath(oldBin)
	}()

	native.SetBinPath("DOESNOTEXIST")

	p := native.NewProcessor()
	err := p.Start()
	if err == nil {
		t.Errorf("processor.Start() got nil, expect error: %v", notFoundError)
	} else if err.Error() != notFoundError {
		t.Errorf("processor.Start() got %v, expect error: %v", err, notFoundError)
	}

	killProcess(p)
}

func TestPidNotStarted(t *testing.T) {
	p := native.NewProcessor()

	pid := p.(*native.Processor).Pid()
	if pid != -1 {
		t.Errorf("processor.Pid() = %d, expect -1", pid)
	}
}

func killProcess(p wingman.Processor) {
	pid := p.(*native.Processor).Pid()
	if pid == -1 {
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}

	proc.Kill()
}
