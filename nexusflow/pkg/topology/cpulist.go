package topology

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParseCPUList parses Linux cpulist format (e.g. "0-3,8,10-15").
func ParseCPUList(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			a, b, ok := strings.Cut(part, "-")
			if !ok {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			lo, err1 := strconv.Atoi(strings.TrimSpace(a))
			hi, err2 := strconv.Atoi(strings.TrimSpace(b))
			if err1 != nil || err2 != nil || lo > hi {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			for i := lo; i <= hi; i++ {
				out = append(out, i)
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid cpu %q: %w", part, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// FormatCPUList encodes sorted unique CPU ids as Linux cpulist (e.g. "0-3,8").
func FormatCPUList(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	seen := map[int]struct{}{}
	var uniq []int
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	sort.Ints(uniq)
	var b strings.Builder
	start := uniq[0]
	prev := uniq[0]
	for i := 1; i <= len(uniq); i++ {
		next := -1
		if i < len(uniq) {
			next = uniq[i]
		}
		if i < len(uniq) && next == prev+1 {
			prev = next
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		if start == prev {
			b.WriteString(strconv.Itoa(start))
		} else {
			fmt.Fprintf(&b, "%d-%d", start, prev)
		}
		if i < len(uniq) {
			start = next
			prev = next
		}
	}
	return b.String()
}
