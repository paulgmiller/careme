package collections

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTop(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []int
		limit int
		want  []int
	}{
		{"replacements", []int{2, 1, 8, 3, 7, 0}, 3, []int{8, 7, 3}},
		{"single", []int{2, 8, 1}, 1, []int{8}},
		{"duplicates", []int{2, 3, 3, 1}, 2, []int{3, 3}},
		{"negative values", []int{-4, -1, -3}, 2, []int{-1, -3}},
		{"large limit", []int{2, 1, 3}, 10, []int{3, 2, 1}},
		{"empty", nil, 3, []int{}},
		{"zero limit", []int{1}, 0, []int{}},
		{"negative limit", []int{1}, -1, []int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := append([]int(nil), tc.items...)
			assert.Equal(t, tc.want, Top(tc.items, tc.limit, func(a, b int) bool { return a < b }))
			assert.Equal(t, original, tc.items)
		})
	}
}

func TestTopCustomOrdering(t *testing.T) {
	items := []string{"b", "a", "c"}
	assert.Equal(t, []string{"a", "b"}, Top(items, 2, func(a, b string) bool { return a > b }))
}
