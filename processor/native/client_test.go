package native

import (
	"os"
	"strconv"
	"testing"

	"github.com/pulchre/wingman/mock"
)

func TestNewClient(t *testing.T) {
	table := []struct {
		in         string
		setPortEnv bool
		port       int
		err        bool
	}{
		{
			in:         "",
			setPortEnv: false,
			port:       DefaultPort,
			err:        false,
		},
		{
			in:         "5555",
			setPortEnv: true,
			port:       5555,
			err:        false,
		},
		{
			in:         "not a port",
			setPortEnv: true,
			err:        true,
		},
	}

	for _, c := range table {
		oldValue, set := os.LookupEnv(PortEnvironmentName)

		if c.setPortEnv {
			os.Setenv(PortEnvironmentName, c.in)
		} else {
			os.Unsetenv(PortEnvironmentName)
		}

		defer func() {
			if set {
				os.Setenv(PortEnvironmentName, oldValue)
			} else {
				os.Unsetenv(PortEnvironmentName)
			}
		}()

		client, err := NewClient()

		if err == nil {
			if c.err {
				t.Fatal("Expected error, got nil")
			}
		} else if err != nil {
			if !c.err {
				t.Fatal("Expected nil error, got: ", err)
			}

			continue
		}

		if client.port != c.port {
			t.Errorf("Expected port %d, got: %d", client.port, c.port)
		}

		if client.pid != os.Getpid() {
			t.Errorf("Expected %d pid, got: %d", os.Getpid(), client.pid)
		}
	}
}

func TestStart(t *testing.T) {
	oldValue, set := os.LookupEnv(PortEnvironmentName)
	defer func() {
		if set {
			os.Setenv(PortEnvironmentName, oldValue)
		} else {
			os.Unsetenv(PortEnvironmentName)
		}
	}()

	os.Setenv(PortEnvironmentName, strconv.Itoa(DefaultPort))

	server := newTestServer(t)
	go server.Start()
	defer server.Shutdown()

	<-server.Sync()

	client, err := NewClient()
	if err != nil {
		t.Fatal("Failed to instantiate client: ", err)
	}

	go func(t *testing.T) {
		err = client.Start()
		if err != nil {
			t.Error("Failed to start client: ", err)
		}
	}(t)

	// Client connected
	<-server.Sync()

	server.SendJob(mock.NewWrappedJob())

	// Job results received
	<-server.Sync()

	server.Shutdown()
	<-server.Done()
}
