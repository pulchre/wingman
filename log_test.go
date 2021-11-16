package wingman

var TestLog *TestLogger

func init() {
	TestLog = &TestLogger{}
	Log = TestLog
}

type TestLogger struct {
	FatalVars [][]interface{}
	PrintVars [][]interface{}
}

func (l *TestLogger) Fatal(v ...interface{}) {
	l.FatalVars = append(l.FatalVars, v)
}

func (l *TestLogger) Fatalf(format string, v ...interface{}) {
	if l.FatalVars == nil {
		l.FatalVars = make([][]interface{}, 0)
	}

	l.FatalVars = append(l.FatalVars, append([]interface{}{format}, v...))
}

func (l *TestLogger) Print(v ...interface{}) {
	l.PrintVars = append(l.PrintVars, v)
}

func (l *TestLogger) Printf(format string, v ...interface{}) {
	if l.PrintVars == nil {
		l.FatalVars = make([][]interface{}, 0)
	}

	l.PrintVars = append(l.PrintVars, append([]interface{}{format}, v...))
}

func (l TestLogger) FatalReceived(v ...interface{}) bool {
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

func (l TestLogger) PrintReceived(v ...interface{}) bool {
	for _, msg := range l.PrintVars {
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

func (l *TestLogger) Reset() {
	l.FatalVars = nil
	l.PrintVars = nil
}
