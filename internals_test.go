package wingman

// This file contains test helpers to access internal data that is not publicly
// exposed.

// ManagerBackend returns the backend for a given manager
func ManagerBackend(s *Manager) Backend {
	return s.backend
}

func SignalReceivedMsg() string             { return signalReceivedMsg }
func SignalHardShutdownMsg() string         { return signalHardShutdownMsg }
func ManagerConcurrency(s *Manager) int     { return s.concurrency }
func PopIdleProcessor(s *Manager) Processor { return s.popIdleProcessor() }

func ProcessorCount(s *Manager) int {
	s.processorCond.L.Lock()
	defer s.processorCond.L.Unlock()

	return len(s.availableProcessors) + len(s.busyProcessors)
}
