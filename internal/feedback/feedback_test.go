package feedback

import "testing"

// The specification defines an explicit status flow. These tests pin the legal
// transitions so a ticket can never skip triage or move backwards unexpectedly.

func TestStatusFlowAllowsDocumentedPath(t *testing.T) {
	legal := [][2]string{
		{"submitted", "triaged"},
		{"triaged", "needs_information"},
		{"needs_information", "submitted"},
		{"triaged", "planned"},
		{"planned", "in_progress"},
		{"in_progress", "released"},
		{"released", "closed"},
		{"triaged", "declined"},
		{"declined", "closed"},
		{"submitted", "closed"},
	}
	for _, pair := range legal {
		if !CanTransition(pair[0], pair[1]) {
			t.Errorf("%s -> %s should be allowed", pair[0], pair[1])
		}
	}
}

func TestStatusFlowBlocksSkipsAndReversals(t *testing.T) {
	illegal := [][2]string{
		{"submitted", "released"},    // cannot release without triaging/planning
		{"submitted", "in_progress"}, // cannot skip planning
		{"released", "in_progress"},  // cannot reopen released work
		{"closed", "triaged"},        // closed is terminal
		{"closed", "submitted"},
		{"planned", "submitted"},
		{"", "triaged"},
		{"submitted", ""},
		{"submitted", "unknown_state"},
	}
	for _, pair := range illegal {
		if CanTransition(pair[0], pair[1]) {
			t.Errorf("%s -> %s must be rejected", pair[0], pair[1])
		}
	}
}

func TestClosedIsTerminal(t *testing.T) {
	if len(NextStatuses("closed")) != 0 {
		t.Errorf("closed must have no next states, got %v", NextStatuses("closed"))
	}
}

func TestEveryStageHasAPathToClosed(t *testing.T) {
	for status := range statusFlow {
		reachable := false
		// Bounded breadth-first walk; the graph is tiny.
		queue := []string{status}
		seen := map[string]bool{}
		for len(queue) > 0 && !reachable {
			current := queue[0]
			queue = queue[1:]
			if seen[current] {
				continue
			}
			seen[current] = true
			if current == "closed" {
				reachable = true
				break
			}
			queue = append(queue, statusFlow[current]...)
		}
		if !reachable {
			t.Errorf("status %q cannot reach closed", status)
		}
	}
}

func TestPqTextArrayEscaping(t *testing.T) {
	if got := pqTextArray([]string{`a"b`, `c\d`}); got != `{"a\"b","c\\d"}` {
		t.Errorf("pqTextArray = %q", got)
	}
	if pqTextArray(nil) != "{}" {
		t.Error("nil slice should render as {}")
	}
}
