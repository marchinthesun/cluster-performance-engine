//go:build !linux

package hugepages

import "fmt"

// SetNrHugepages is a no-op stub outside Linux.
func SetNrHugepages(pagesize string, n int) (int, error) {
	return 0, fmt.Errorf("hugepages: Linux only")
}
