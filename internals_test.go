package wingman

// This file contains test helpers to access internal data that is not publicly
// exposed.

// ManagerBackend returns the backend for a given manager
func ManagerBackend(s *Manager) Backend {
	return s.backend
}

func SignalReceivedMsg() string     { return signalReceivedMsg }
func SignalHardShutdownMsg() string { return signalHardShutdownMsg }
