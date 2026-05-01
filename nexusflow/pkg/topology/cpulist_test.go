package topology

import (
	"reflect"
	"testing"
)

func TestParseCPUList(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []int
	}{
		{"0", []int{0}},
		{"0-3", []int{0, 1, 2, 3}},
		{"0-1,8,10-11", []int{0, 1, 8, 10, 11}},
	} {
		got, err := ParseCPUList(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: got %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestFormatCPUList(t *testing.T) {
	for _, tc := range []struct {
		in   []int
		want string
	}{
		{[]int{}, ""},
		{[]int{0}, "0"},
		{[]int{0, 1, 2, 4}, "0-2,4"},
		{[]int{8, 8, 9}, "8-9"},
	} {
		got := FormatCPUList(tc.in)
		if got != tc.want {
			t.Fatalf("%v: got %q want %q", tc.in, got, tc.want)
		}
	}
}
