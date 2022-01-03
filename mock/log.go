package mock

import (
	"sync"

	"github.com/pulchre/wingman"
)

var TestLog *TestLogger

func init() {
	TestLog = &TestLogger{}
	wingman.Log = TestLog
}

type TestLogger struct {
	FatalVars [][]interface{}
	PrintVars [][]interface{}
	mu        sync.Mutex
}

func (l *TestLogger) Fatal(v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.FatalVars = append(l.FatalVars, v)
}

func (l *TestLogger) Fatalf(format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.FatalVars == nil {
		l.FatalVars = make([][]interface{}, 0)
	}

	l.FatalVars = append(l.FatalVars, append([]interface{}{format}, v...))
}

func (l *TestLogger) Print(v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.PrintVars = append(l.PrintVars, v)
}

func (l *TestLogger) Printf(format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.PrintVars == nil {
		l.FatalVars = make([][]interface{}, 0)
	}

	l.PrintVars = append(l.PrintVars, append([]interface{}{format}, v...))
}

func (l *TestLogger) FatalReceived(v ...interface{}) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, msg := range l.FatalVars {
		if len(msg) != len(v) {
			continue
		}

		for j, val := range v {
			if val == msg[j] {
				if j == len(v)-1 {
					return true
				}
			} else {
				break
			}
		}
	}

	return false
}

func (l *TestLogger) PrintReceived(v ...interface{}) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, msg := range l.PrintVars {
		if len(msg) != len(v) && len(v) != 1 {
			continue
		}

		if len(v) == 1 && msg[0] == v[0] {
			return true
		}

		for j, val := range v {
			if val == msg[j] {
				if j == len(v)-1 {
					return true
				}
			} else {
				break
			}
		}
	}

	return false
}

func (l *TestLogger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.FatalVars = nil
	l.PrintVars = nil
}
