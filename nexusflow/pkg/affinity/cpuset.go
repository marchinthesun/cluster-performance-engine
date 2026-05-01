package affinity

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CPUsToTasksetSpec converts logical CPU ids to taskset -c argument (ranges compacted).
func CPUsToTasksetSpec(cpuIDs []int) string {
	if len(cpuIDs) == 0 {
		return ""
	}
	seen := map[int]struct{}{}
	var xs []int
	for _, id := range cpuIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		xs = append(xs, id)
	}
	sort.Ints(xs)

	var parts []string
	for i := 0; i < len(xs); {
		j := i + 1
		for j < len(xs) && xs[j] == xs[j-1]+1 {
			j++
		}
		if j == i+1 {
			parts = append(parts, strconv.Itoa(xs[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", xs[i], xs[j-1]))
		}
		i = j
	}
	return strings.Join(parts, ",")
}
