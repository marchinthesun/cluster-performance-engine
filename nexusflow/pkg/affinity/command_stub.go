//go:build !linux

package affinity

import (
	"fmt"
	"os/exec"
)

func Command(cpuIDs []int, argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty command argv")
	}
	if len(cpuIDs) > 0 {
		return nil, fmt.Errorf("subprocess pinning requires Linux and util-linux taskset")
	}
	return exec.Command(argv[0], argv[1:]...), nil
}
