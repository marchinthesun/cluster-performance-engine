//go:build linux

package hugepages

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SysfsPath maps pagesize label to nr_hugepages sysfs path.
func SysfsPath(pagesize string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(pagesize)) {
	case "2m", "2mib", "2048":
		return "/sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages", nil
	case "1g", "1gib", "1048576":
		return "/sys/kernel/mm/hugepages/hugepages-1048576kB/nr_hugepages", nil
	default:
		return "", fmt.Errorf("hugepages: unknown pagesize %q (use 2M or 1G)", pagesize)
	}
}

// SetNrHugepages writes the target total count of huge pages of the given size.
func SetNrHugepages(pagesize string, n int) (int, error) {
	if n < 0 {
		return 0, fmt.Errorf("hugepages: negative count")
	}
	path, err := SysfsPath(pagesize)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(n)+"\n"), 0); err != nil {
		return 0, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return n, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return n, nil
	}
	return v, nil
}
