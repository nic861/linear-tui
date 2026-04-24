package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// testDetailsTags returns a concrete ThemeTags value for details-header tests.
// Using fixed tag strings keeps output assertions independent of NewThemeTags().
func testDetailsTags() ThemeTags {
	return ThemeTags{
		Foreground:    "[white]",
		SecondaryText: "[gray]",
		HeaderText:    "[#a0a0a0]",
		Accent:        "[#5e6ad2]",
		Border:        "[#3c3c3c]",
		Warning:       "[yellow]",
		Error:         "[red]",
	}
}

// joinHeader concatenates the header lines so substring/regex assertions operate
// on the rendered payload that SetText would receive.
func joinHeader(lines []string) string {
	return strings.Join(lines, "\n")
}

// TestBuildDetailsHeader_BlockedBySection verifies a "Blocked by:" section appears
// when BlockedBy is non-empty (and no "Blocks:" section when Blocks is empty).
// Covers AC-008 (T-006).
func TestBuildDetailsHeader_BlockedBySection(t *testing.T) {
	issue := &linearapi.Issue{
		ID:         "issue-1",
		Identifier: "EFF-1",
		Title:      "Downstream feature",
		State:      "Todo",
		Priority:   3,
		BlockedBy: []linearapi.IssueRelationRef{
			{ID: "b-1", Identifier: "EFF-239", Title: "Centralize scope resolution", State: "Todo", StateType: "unstarted"},
		},
		Blocks: nil,
	}

	lines := buildDetailsHeader(issue, testDetailsTags(), 1)
	joined := joinHeader(lines)

	if matched, err := regexp.MatchString(`Blocked by:.*1 items`, joined); err != nil || !matched {
		t.Errorf("expected header line matching /Blocked by:.*1 items/, got:\n%s", joined)
	}
	if !strings.Contains(joined, "EFF-239") {
		t.Errorf("expected blocker identifier EFF-239 in header, got:\n%s", joined)
	}
	if !strings.Contains(joined, "Centralize scope resolution") {
		t.Errorf("expected blocker title in header, got:\n%s", joined)
	}
	if strings.Contains(joined, "Blocks:") {
		t.Errorf("did not expect Blocks: section when Blocks slice is empty, got:\n%s", joined)
	}
}

// TestBuildDetailsHeader_BlocksSection verifies a "Blocks:" section appears when
// Blocks is non-empty (and no "Blocked by:" when BlockedBy is empty).
// Covers AC-009 (T-007).
func TestBuildDetailsHeader_BlocksSection(t *testing.T) {
	issue := &linearapi.Issue{
		ID:         "issue-1",
		Identifier: "EFF-1",
		Title:      "Upstream feature",
		State:      "In Progress",
		Priority:   2,
		BlockedBy:  nil,
		Blocks: []linearapi.IssueRelationRef{
			{ID: "f-1", Identifier: "EFF-244", Title: "Feature Y", State: "In Progress", StateType: "started"},
		},
	}

	lines := buildDetailsHeader(issue, testDetailsTags(), 1)
	joined := joinHeader(lines)

	if matched, err := regexp.MatchString(`Blocks:.*1 items`, joined); err != nil || !matched {
		t.Errorf("expected header line matching /Blocks:.*1 items/, got:\n%s", joined)
	}
	if !strings.Contains(joined, "EFF-244") {
		t.Errorf("expected target identifier EFF-244 in header, got:\n%s", joined)
	}
	if !strings.Contains(joined, "Feature Y") {
		t.Errorf("expected target title in header, got:\n%s", joined)
	}
	if strings.Contains(joined, "Blocked by:") {
		t.Errorf("did not expect Blocked by: section when BlockedBy slice is empty, got:\n%s", joined)
	}
}

// TestBuildDetailsHeader_EmptyRelationsSkipped verifies both section headers are
// hidden when BlockedBy and Blocks are both empty. Covers AC-010 (T-008).
func TestBuildDetailsHeader_EmptyRelationsSkipped(t *testing.T) {
	issue := &linearapi.Issue{
		ID:         "issue-1",
		Identifier: "EFF-1",
		Title:      "Standalone",
		State:      "Todo",
		Priority:   3,
		BlockedBy:  []linearapi.IssueRelationRef{},
		Blocks:     []linearapi.IssueRelationRef{},
	}

	lines := buildDetailsHeader(issue, testDetailsTags(), 1)
	joined := joinHeader(lines)

	if strings.Contains(joined, "Blocked by:") {
		t.Errorf("expected no Blocked by: section for empty BlockedBy, got:\n%s", joined)
	}
	if strings.Contains(joined, "Blocks:") {
		t.Errorf("expected no Blocks: section for empty Blocks, got:\n%s", joined)
	}
}

// TestBuildDetailsHeader_PreservesExisting asserts the refactor does not drop
// any pre-existing sections. Covers AC-011 and FR-006's byte-identical guarantee
// for pre-existing rendering (T-009).
func TestBuildDetailsHeader_PreservesExisting(t *testing.T) {
	parent := &linearapi.IssueRef{
		ID:         "p-1",
		Identifier: "EFF-100",
		Title:      "Parent feature",
	}
	issue := &linearapi.Issue{
		ID:         "issue-1",
		Identifier: "EFF-200",
		Title:      "Thing",
		State:      "In Progress",
		Assignee:   "Nic",
		Priority:   2,
		CycleName:  "Cycle 3",
		Labels: []linearapi.IssueLabel{
			{ID: "l-1", Name: "bug"},
			{ID: "l-2", Name: "priority-high"},
		},
		Parent: parent,
		Children: []linearapi.IssueChildRef{
			{ID: "c-1", Identifier: "EFF-201", Title: "Sub one", State: "Todo"},
		},
	}

	lines := buildDetailsHeader(issue, testDetailsTags(), 1)
	joined := joinHeader(lines)

	wantSubstrings := []string{
		"State:",
		"Assignee:",
		"Priority:",
		"Labels:",
		"Parent:",
		"Sub-issues:",
		"EFF-100", // parent identifier
		"EFF-201", // child identifier
		"Sub one", // child title (confirms at least one child row rendered)
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(joined, want) {
			t.Errorf("header missing expected substring %q; got:\n%s", want, joined)
		}
	}
}

// TestBuildDetailsHeader_NilRelationsNoHeader is the invalidation test for ASM-004:
// if a future refactor gates on `!= nil` instead of `len(...) > 0`, this catches it.
// Both nil and explicitly-empty slice cases must produce no section header.
// Covers T-INV-004.
func TestBuildDetailsHeader_NilRelationsNoHeader(t *testing.T) {
	tags := testDetailsTags()

	t.Run("nil slices", func(t *testing.T) {
		issue := &linearapi.Issue{
			Identifier: "EFF-1",
			Title:      "T",
			State:      "Todo",
			BlockedBy:  nil,
			Blocks:     nil,
		}
		joined := joinHeader(buildDetailsHeader(issue, tags, 1))
		if strings.Contains(joined, "Blocked by:") {
			t.Errorf("nil BlockedBy must not produce Blocked by: section; got:\n%s", joined)
		}
		if strings.Contains(joined, "Blocks:") {
			t.Errorf("nil Blocks must not produce Blocks: section; got:\n%s", joined)
		}
	})

	t.Run("explicitly empty slices", func(t *testing.T) {
		issue := &linearapi.Issue{
			Identifier: "EFF-2",
			Title:      "T",
			State:      "Todo",
			BlockedBy:  []linearapi.IssueRelationRef{},
			Blocks:     []linearapi.IssueRelationRef{},
		}
		joined := joinHeader(buildDetailsHeader(issue, tags, 1))
		if strings.Contains(joined, "Blocked by:") {
			t.Errorf("empty BlockedBy must not produce Blocked by: section; got:\n%s", joined)
		}
		if strings.Contains(joined, "Blocks:") {
			t.Errorf("empty Blocks must not produce Blocks: section; got:\n%s", joined)
		}
	})
}
