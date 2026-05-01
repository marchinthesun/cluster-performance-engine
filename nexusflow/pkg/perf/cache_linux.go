//go:build linux

package perf

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	perfTypeHWCache       = 3
	hwCacheLL             = 2 // last-level / L3-class
	hwCacheOpRead         = 0
	hwCacheResultAccess   = 1
	hwCacheResultMiss     = 2
)

func hwCacheConfig(cacheID, opID, resultID uint64) uint64 {
	return cacheID | (opID << 8) | (resultID << 16)
}

// OpenLLCLastLevelMiss opens a perf counter for last-level cache read misses (often L3 on many CPUs).
func OpenLLCLastLevelMiss(pid, cpu int) (*Counter, error) {
	return openHWCache(pid, cpu, hwCacheConfig(hwCacheLL, hwCacheOpRead, hwCacheResultMiss))
}

// OpenLLCLastLevelAccess opens last-level cache read accesses (hits+misses proxy).
func OpenLLCLastLevelAccess(pid, cpu int) (*Counter, error) {
	return openHWCache(pid, cpu, hwCacheConfig(hwCacheLL, hwCacheOpRead, hwCacheResultAccess))
}

func openHWCache(pid, cpu int, config uint64) (*Counter, error) {
	attr := unix.PerfEventAttr{
		Type:   perfTypeHWCache,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		Config: config,
	}
	fd, err := unix.PerfEventOpen(&attr, pid, cpu, -1, 0)
	if err != nil {
		return nil, fmt.Errorf("perf_event_open hw_cache: %w", err)
	}
	return &Counter{fd: fd}, nil
}
