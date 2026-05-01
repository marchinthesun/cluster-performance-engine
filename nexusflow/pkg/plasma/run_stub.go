//go:build !linux

package plasma

import (
	"context"
	"fmt"

	"github.com/kube-metrics/nexusflow/pkg/topology"
)

// Run is only supported on Linux (unix sockets, shm, SCM_RIGHTS).
func Run(ctx context.Context, cfg Config, _ *topology.Topology) error {
	return fmt.Errorf("plasma: Linux only (got stub build)")
}
