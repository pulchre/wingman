package mock

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/pulchre/wingman"
)

var TestLog *TestLogger

func init() {
	TestLog = &TestLogger{
		FatalVars: make([]string, 0),
		PrintVars: make([]string, 0),
	}
	wingman.Log = TestLog
}

type TestLogger struct {
	FatalVars []string
	PrintVars []string
	mu        sync.Mutex
}

func (l *TestLogger) Fatal(v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.FatalVars == nil {
		l.FatalVars = make([]string, 0)
	}

	l.FatalVars = append(l.FatalVars, fmt.Sprint(v...))
}

func (l *TestLogger) Fatalf(format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.FatalVars == nil {
		l.FatalVars = make([]string, 0)
	}

	l.FatalVars = append(l.FatalVars, fmt.Sprintf(format, v...))
}

func (l *TestLogger) Print(v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.PrintVars == nil {
		l.PrintVars = make([]string, 0)
	}

	l.PrintVars = append(l.PrintVars, fmt.Sprint(v...))
}

func (l *TestLogger) Printf(format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.PrintVars == nil {
		l.PrintVars = make([]string, 0)
	}

	l.PrintVars = append(l.PrintVars, fmt.Sprintf(format, v...))
}

func (l *TestLogger) FatalReceived(v string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, msg := range l.FatalVars {
		if v == msg {
			return true
		}
	}

	return false
}

func (l *TestLogger) PrintReceived(v string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, msg := range l.PrintVars {
		if v == msg {
			return true
		}
	}

	return false
}

func (l *TestLogger) PrintReceivedRegex(v string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	r := regexp.MustCompile(v)

	for _, msg := range l.PrintVars {
		if r.MatchString(msg) {
			return true
		}
	}

	return false
}

func (l *TestLogger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.FatalVars = make([]string, 0)
	l.PrintVars = make([]string, 0)
}
