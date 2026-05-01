package dashboard

import "testing"

func TestBinaryVersion_nonempty(t *testing.T) {
	v := BinaryVersion()
	if v == "" {
		t.Fatal("BinaryVersion returned empty string")
	}
}
