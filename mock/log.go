package mock

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/pulchre/wingman"
	"github.com/rs/zerolog"
)

var TestLog *TestLogger

func init() {
	ResetLog()
}

func ResetLog() {
	TestLog = &TestLogger{}
	TestLog.Logger = zerolog.New(TestLog)
	wingman.Log = TestLog
}

type TestLogger struct {
	zerolog.Logger
	events []Event

	mu sync.RWMutex
}

type Event map[string]any

func (l *TestLogger) Fatal() *zerolog.Event {
	return l.Error()
}

func (l *TestLogger) Write(p []byte) (n int, err error) {
	n = len(p)

	var event Event
	err = json.Unmarshal(p, &event)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.events = append(l.events, event)

	return
}

func (l *TestLogger) AddEvent(event Event) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.events = append(l.events, event)
}

func (l *TestLogger) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return len(l.events)
}

func (l *TestLogger) EventAtIndex(i int) Event {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.events[i]
}

func (l *TestLogger) EventByMessage(msg string) (Event, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, e := range l.events {
		if m, ok := e["message"]; ok && m == msg {
			return e, true
		}
	}

	return Event{}, false
}

func (e Event) HasField(key string) bool {
	_, ok := e[key]
	return ok
}

func (e Event) HasErr(err error) bool {
	return e["error"] == err.Error()
}

func (e Event) HasStr(key, value string) bool {
	return e[key] == value
}

func (e Event) HasTime(key string, t time.Time, within time.Duration) bool {
	str, ok := e[key]
	if !ok {
		return false
	}

	switch str.(type) {
	case string:
		v, err := time.Parse(time.RFC3339, str.(string))
		if err != nil {
			return false
		}

		if t.Add(within).After(v) && t.Add(-within).Before(v) {
			return true
		}
	default:
		return false
	}

	return false
}
