//go:build linux

package affinity

import (
	"fmt"
	"os/exec"
)

// Command builds an exec.Cmd that runs argv under util-linux taskset -c … when cpuIDs is non-empty.
// when cpuIDs is non-empty (allows DAG / subprocess workflows without syscall.Exec).
func Command(cpuIDs []int, argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty command argv")
	}
	if len(cpuIDs) == 0 {
		return exec.Command(argv[0], argv[1:]...), nil
	}
	if _, err := exec.LookPath("taskset"); err != nil {
		return nil, fmt.Errorf("taskset required for subprocess pinning: %w", err)
	}
	spec := CPUsToTasksetSpec(cpuIDs)
	if spec == "" {
		return exec.Command(argv[0], argv[1:]...), nil
	}
	// No trailing "--": some minimal taskset implementations mis-parse it as the program name.
	args := append([]string{"taskset", "-c", spec}, argv...)
	return exec.Command(args[0], args[1:]...), nil
}
