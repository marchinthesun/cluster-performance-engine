package affinity

import "testing"

func TestCPUsToTasksetSpec(t *testing.T) {
	for _, tc := range []struct {
		in   []int
		want string
	}{
		{[]int{0}, "0"},
		{[]int{0, 1, 2}, "0-2"},
		{[]int{0, 1, 2, 8}, "0-2,8"},
		{[]int{8, 0, 2, 1}, "0-2,8"},
	} {
		got := CPUsToTasksetSpec(tc.in)
		if got != tc.want {
			t.Fatalf("%v: got %q want %q", tc.in, got, tc.want)
		}
	}
}
