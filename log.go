package wingman

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

type Logger interface {
	Panic() *zerolog.Event
	Err(error) *zerolog.Event
	Fatal() *zerolog.Event
	Info() *zerolog.Event
}

var Log Logger

func init() {
	l := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339Nano}).
		With().
		Timestamp().
		Logger().
		Level(zerolog.InfoLevel).
		Hook(zerolog.HookFunc(func(e *zerolog.Event, _ zerolog.Level, _ string) {
			e.Int("pid", os.Getpid())
		}))

	Log = &l
}
