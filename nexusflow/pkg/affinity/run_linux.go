//go:build linux

package affinity

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// Run replaces the current process after pinning to cpuIDs (Linux only).
func Run(cpuIDs []int, argv []string, opts RunOpts) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty command")
	}
	if len(cpuIDs) == 0 {
		return fmt.Errorf("empty CPU list")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("lookpath %q: %w", argv[0], err)
	}

	var set unix.CPUSet
	set.Zero()
	for _, id := range cpuIDs {
		if id < 0 {
			return fmt.Errorf("invalid cpu id %d", id)
		}
		set.Set(id)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if opts.Nice != nil {
		if err := unix.Setpriority(unix.PRIO_PROCESS, 0, *opts.Nice); err != nil {
			return fmt.Errorf("setpriority: %w", err)
		}
	}
	if err := unix.SchedSetaffinity(0, &set); err != nil {
		return fmt.Errorf("sched_setaffinity: %w", err)
	}

	return syscall.Exec(path, argv, os.Environ())
}
