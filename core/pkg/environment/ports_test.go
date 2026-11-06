package environment

import (
	"slices"
	"testing"
)

// Operational ports read as ranges and single ports, sorted, each once; a
// port out of 1..65535, a range that runs backwards or a word is refused.
func TestParsePorts(t *testing.T) {
	for raw, want := range map[string][]int{
		"":                   {},
		" ":                  {},
		"42000":              {42000},
		"41000-41003, 42000": {41000, 41001, 41002, 41003, 42000},
		"5-6,6,1-2,":         {1, 2, 5, 6},
		"65535":              {65535},
		"41000 - 41001":      {41000, 41001},
	} {
		got, err := ParsePorts(raw)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%q: %v %v, want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "65536", "41003-41000", "socks", "1-", "-5", "1-2-3"} {
		if got, err := ParsePorts(raw); err == nil {
			t.Errorf("%q: taken as %v", raw, got)
		}
	}
}
