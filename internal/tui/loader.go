package tui

import (
	"cmp"
	"slices"

	"github.com/moby/moby/api/types/container"

	"github.com/spar/dpad/internal/sortx"
)

// loader keeps a list to one request at a time. A reload asked for while one
// is running is remembered and started when it ends.
type loader struct{ inFlight, pending bool }

// begin reports whether a request may start now.
func (l *loader) begin() bool {
	if l.inFlight {
		l.pending = true
		return false
	}
	l.inFlight = true
	return true
}

// end marks the request finished and reports whether to load again.
func (l *loader) end() bool {
	again := l.pending
	l.inFlight, l.pending = false, false
	return again
}

// containerUsage is the Containers tab's latest snapshot, which other tabs
// use to show what refers to their rows.
type containerUsage struct {
	containers  []container.Summary
	usageLoaded bool
	usageErr    error
}

func (u *containerUsage) setContainers(msg containersMsg) {
	u.containers, u.usageErr, u.usageLoaded = msg.items, msg.err, true
}

func (u *containerUsage) usageKnown() bool { return u.usageLoaded && u.usageErr == nil }

// sortByName sorts a list that has only a name order.
func sortByName[T any](name func(T) string) func([]T, sortx.Key, bool) {
	return func(items []T, _ sortx.Key, reverse bool) {
		slices.SortFunc(items, func(a, b T) int {
			if reverse {
				return cmp.Compare(name(b), name(a))
			}
			return cmp.Compare(name(a), name(b))
		})
	}
}
