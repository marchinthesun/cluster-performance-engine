package plasma

import "time"

// Config drives the Plasma coordinator (unix socket + DAG engine).
type Config struct {
	SocketPath   string
	PipelinePath string
	ShmName      string
	ShmSize      int64
	IdleExit     time.Duration
}
