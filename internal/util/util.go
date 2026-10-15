// Package util holds the small helpers several packages need, so each has
// one definition.
package util

import (
	"fmt"
	"slices"
)

// Plural counts a noun: "1 status", "3 statuses".
func Plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// AppendNew appends v to l unless l has it already.
func AppendNew(l []string, v string) []string {
	if slices.Contains(l, v) {
		return l
	}
	return append(l, v)
}
