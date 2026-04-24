package tui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestRenderIssueRow(t *testing.T) {
	// renderIssueRow returns: [identifier, cycleName, state, priorityText, project, title]
	tests := []struct {
		name      string
		issue     linearapi.Issue
		wantLen   int
		wantID    string
		wantCycle string
		wantState string
	}{
		{
			name: "normal issue",
			issue: linearapi.Issue{
				ID:         "test-1",
				Identifier: "LIN-1",
				Title:      "Test Issue",
				State:      "Todo",
				Assignee:   "John Doe",
				Priority:   3, // Normal priority
			},
			wantLen:   6,
			wantID:    "LIN-1",
			wantCycle: "-",
			wantState: "Todo",
		},
		{
			name: "issue with cycle",
			issue: linearapi.Issue{
				ID:         "test-1b",
				Identifier: "LIN-1B",
				Title:      "Test Issue With Cycle",
				State:      "Todo",
				Assignee:   "John Doe",
				Priority:   3,
				CycleName:  "Cycle 3",
			},
			wantLen:   6,
			wantID:    "LIN-1B",
			wantCycle: "Cycle 3",
			wantState: "Todo",
		},
		{
			name: "unassigned issue",
			issue: linearapi.Issue{
				ID:         "test-2",
				Identifier: "LIN-2",
				Title:      "Another Issue",
				State:      "In Progress",
				Assignee:   "",
				Priority:   2, // High priority
			},
			wantLen:   6,
			wantID:    "LIN-2",
			wantCycle: "-",
			wantState: "In Progres", // truncated to 10 chars
		},
		{
			name: "long identifier truncated",
			issue: linearapi.Issue{
				ID:         "test-3",
				Identifier: "VERY-LONG-IDENTIFIER-123",
				Title:      "Long ID Issue",
				State:      "Done",
				Assignee:   "Jane",
				Priority:   1, // Urgent priority
			},
			wantLen:   6,
			wantID:    "VERY-LONG-", // truncated to 10 chars
			wantCycle: "-",
			wantState: "Done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := renderIssueRow(tt.issue)
			if len(row) != tt.wantLen {
				t.Errorf("renderIssueRow() length = %d, want %d", len(row), tt.wantLen)
			}
			if len(row) > 0 && row[0] != tt.wantID {
				t.Errorf("renderIssueRow()[0] = %q, want %q", row[0], tt.wantID)
			}
			if len(row) > 1 && row[1] != tt.wantCycle {
				t.Errorf("renderIssueRow()[1] = %q, want %q", row[1], tt.wantCycle)
			}
			if len(row) > 2 && row[2] != tt.wantState {
				t.Errorf("renderIssueRow()[2] = %q, want %q", row[2], tt.wantState)
			}
			// Column 4 is Project (empty = "-")
			if len(row) > 4 && tt.issue.ProjectName == "" && row[4] != "-" {
				t.Errorf("renderIssueRow()[4] = %q, want %q", row[4], "-")
			}
		})
	}
}

func TestRenderIssueRow_Truncation(t *testing.T) {
	issue := linearapi.Issue{
		ID:         "test",
		Identifier: "ABCDEFGHIJKLMNOP", // 16 chars
		Title:      "Test",
		State:      "ABCDEFGHIJKLMNOP", // 16 chars
		Assignee:   "ABCDEFGHIJKLMNOP", // 16 chars
		Priority:   1,
		CycleName:  "ABCDEFGHIJKLMNOPQRSTUV", // 22 chars
		UpdatedAt:  time.Now(),
	}

	row := renderIssueRow(issue)

	// Identifier should be truncated to 10 chars
	if len(row[0]) > 10 {
		t.Errorf("Identifier length = %d, want <= 10", len(row[0]))
	}

	// Cycle should be truncated to 16 chars
	if len(row[1]) > 16 {
		t.Errorf("Cycle length = %d, want <= 16", len(row[1]))
	}

	// State should be truncated to 10 chars
	if len(row[2]) > 10 {
		t.Errorf("State length = %d, want <= 10", len(row[2]))
	}
}

// TestIsBlocked covers the isBlocked predicate across empty, active, terminal,
// and mixed blocker lists. Covers AC-005, AC-006, AC-007 (T-003).
func TestIsBlocked(t *testing.T) {
	tests := []struct {
		name string
		in   *linearapi.Issue
		want bool
	}{
		{
			name: "nil BlockedBy returns false",
			in:   &linearapi.Issue{BlockedBy: nil},
			want: false,
		},
		{
			name: "empty BlockedBy returns false",
			in:   &linearapi.Issue{BlockedBy: []linearapi.IssueRelationRef{}},
			want: false,
		},
		{
			name: "one active blocker (started) returns true",
			in: &linearapi.Issue{BlockedBy: []linearapi.IssueRelationRef{
				{Identifier: "A-1", StateType: "started"},
			}},
			want: true,
		},
		{
			name: "one active blocker (backlog) returns true",
			in: &linearapi.Issue{BlockedBy: []linearapi.IssueRelationRef{
				{Identifier: "A-2", StateType: "backlog"},
			}},
			want: true,
		},
		{
			name: "one active blocker (unstarted) returns true",
			in: &linearapi.Issue{BlockedBy: []linearapi.IssueRelationRef{
				{Identifier: "A-3", StateType: "unstarted"},
			}},
			want: true,
		},
		{
			name: "all terminal blockers (completed + canceled) returns false",
			in: &linearapi.Issue{BlockedBy: []linearapi.IssueRelationRef{
				{Identifier: "C-1", StateType: "completed"},
				{Identifier: "C-2", StateType: "canceled"},
			}},
			want: false,
		},
		{
			name: "mixed: one active among terminals returns true",
			in: &linearapi.Issue{BlockedBy: []linearapi.IssueRelationRef{
				{Identifier: "C-1", StateType: "completed"},
				{Identifier: "A-1", StateType: "started"},
				{Identifier: "C-2", StateType: "canceled"},
			}},
			want: true,
		},
		{
			name: "empty StateType treated as active (conservative)",
			in: &linearapi.Issue{BlockedBy: []linearapi.IssueRelationRef{
				{Identifier: "U-1", StateType: ""},
			}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBlocked(tt.in)
			if got != tt.want {
				t.Errorf("isBlocked(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestIsBlocked_TerminalStatesFalse is the focused invalidation test for ASM-003:
// if someone removes or negates the StateType gate, an all-terminal BlockedBy would
// start returning true and this test fails. Covers T-INV-003.
func TestIsBlocked_TerminalStatesFalse(t *testing.T) {
	issue := &linearapi.Issue{
		BlockedBy: []linearapi.IssueRelationRef{
			{Identifier: "DONE-1", State: "Done", StateType: "completed"},
			{Identifier: "CAN-1", State: "Canceled", StateType: "canceled"},
		},
	}
	if isBlocked(issue) {
		t.Errorf("isBlocked = true, want false when all blockers are {completed, canceled}")
	}
}

// TestRenderIdentifierCell_BlockedColor verifies the identifier cell takes the
// StatusBlocked color iff the issue has at least one active blocker.
// Covers AC-012, AC-013, AC-014 (T-005).
func TestRenderIdentifierCell_BlockedColor(t *testing.T) {
	blockedColor := tcell.NewRGBColor(200, 80, 80)
	secondaryColor := tcell.NewRGBColor(120, 120, 120)
	theme := Theme{
		SecondaryText: secondaryColor,
		StatusBlocked: blockedColor,
	}
	row := IssueRow{Level: 0, HasChildren: false, IsExpanded: false}

	t.Run("active blocker → StatusBlocked color", func(t *testing.T) {
		issue := &linearapi.Issue{
			Identifier: "EFF-1",
			BlockedBy: []linearapi.IssueRelationRef{
				{Identifier: "BL-1", StateType: "started"},
			},
		}
		cell := renderIdentifierCell(issue, theme, row)
		if cell == nil {
			t.Fatalf("renderIdentifierCell returned nil")
		}
		fg, _, _ := cell.Style.Decompose()
		if fg != blockedColor {
			t.Errorf("cell.Style foreground = %v, want StatusBlocked %v", fg, blockedColor)
		}
		if cell.Text != " EFF-1" {
			t.Errorf("cell.Text = %q, want %q", cell.Text, " EFF-1")
		}
	})

	t.Run("no blockers → SecondaryText color", func(t *testing.T) {
		issue := &linearapi.Issue{
			Identifier: "EFF-2",
			BlockedBy:  nil,
		}
		cell := renderIdentifierCell(issue, theme, row)
		if cell == nil {
			t.Fatalf("renderIdentifierCell returned nil")
		}
		fg, _, _ := cell.Style.Decompose()
		if fg != secondaryColor {
			t.Errorf("cell.Style foreground = %v, want SecondaryText %v (no blockers)", fg, secondaryColor)
		}
		if fg == blockedColor {
			t.Errorf("cell.Style foreground should NOT be StatusBlocked when BlockedBy is empty")
		}
	})

	t.Run("only terminal blockers → SecondaryText color", func(t *testing.T) {
		issue := &linearapi.Issue{
			Identifier: "EFF-3",
			BlockedBy: []linearapi.IssueRelationRef{
				{Identifier: "DONE-1", StateType: "completed"},
			},
		}
		cell := renderIdentifierCell(issue, theme, row)
		if cell == nil {
			t.Fatalf("renderIdentifierCell returned nil")
		}
		fg, _, _ := cell.Style.Decompose()
		if fg != secondaryColor {
			t.Errorf("cell.Style foreground = %v, want SecondaryText %v (all blockers terminal)", fg, secondaryColor)
		}
	})
}

// TestRenderIdentifierCell_TerminalBlockersNotColored is the invalidation test for ASM-005:
// if someone changes the color gate from isBlocked() to len(BlockedBy) > 0, terminal
// blockers would start coloring rows and this test fails. Covers T-INV-005.
func TestRenderIdentifierCell_TerminalBlockersNotColored(t *testing.T) {
	blockedColor := tcell.NewRGBColor(200, 80, 80)
	secondaryColor := tcell.NewRGBColor(120, 120, 120)
	theme := Theme{
		SecondaryText: secondaryColor,
		StatusBlocked: blockedColor,
	}
	issue := &linearapi.Issue{
		Identifier: "EFF-9",
		BlockedBy: []linearapi.IssueRelationRef{
			{Identifier: "DONE-1", StateType: "completed"},
		},
	}
	cell := renderIdentifierCell(issue, theme, IssueRow{Level: 0})
	if cell == nil {
		t.Fatalf("renderIdentifierCell returned nil")
	}
	fg, _, _ := cell.Style.Decompose()
	if fg == blockedColor {
		t.Errorf("cell.Style foreground = StatusBlocked, want SecondaryText — terminal-only blockers must not trigger the blocked color")
	}
	if fg != secondaryColor {
		t.Errorf("cell.Style foreground = %v, want SecondaryText %v", fg, secondaryColor)
	}
}

// TestThemes_StatusBlockedSet verifies every theme variant declares a valid StatusBlocked
// color, the spec-mandated RGB values, and that ColorBlindTheme uses a distinct hue
// (not red) from the other variants. Covers AC-015, FR-010, FR-011 (T-010).
func TestThemes_StatusBlockedSet(t *testing.T) {
	wantLinear := tcell.NewRGBColor(255, 140, 0)
	wantHighContrast := tcell.NewRGBColor(255, 140, 0)
	wantColorBlind := tcell.NewRGBColor(204, 121, 167)

	if !LinearTheme.StatusBlocked.Valid() {
		t.Errorf("LinearTheme.StatusBlocked is not a valid tcell.Color")
	}
	if LinearTheme.StatusBlocked != wantLinear {
		t.Errorf("LinearTheme.StatusBlocked = %v, want %v (bright orange, distinct from Canceled red)", LinearTheme.StatusBlocked, wantLinear)
	}

	if !HighContrastTheme.StatusBlocked.Valid() {
		t.Errorf("HighContrastTheme.StatusBlocked is not a valid tcell.Color")
	}
	if HighContrastTheme.StatusBlocked != wantHighContrast {
		t.Errorf("HighContrastTheme.StatusBlocked = %v, want %v (bright orange, distinct from Canceled red)", HighContrastTheme.StatusBlocked, wantHighContrast)
	}

	if !ColorBlindTheme.StatusBlocked.Valid() {
		t.Errorf("ColorBlindTheme.StatusBlocked is not a valid tcell.Color")
	}
	if ColorBlindTheme.StatusBlocked != wantColorBlind {
		t.Errorf("ColorBlindTheme.StatusBlocked = %v, want %v (#CC79A7 — Okabe-Ito reddish-purple)", ColorBlindTheme.StatusBlocked, wantColorBlind)
	}

	// AC-015: ColorBlindTheme must not reuse the red of the other two variants.
	if ColorBlindTheme.StatusBlocked == LinearTheme.StatusBlocked {
		t.Errorf("ColorBlindTheme.StatusBlocked reuses LinearTheme.StatusBlocked — must be a distinct hue (not red)")
	}
	if ColorBlindTheme.StatusBlocked == HighContrastTheme.StatusBlocked {
		t.Errorf("ColorBlindTheme.StatusBlocked reuses HighContrastTheme.StatusBlocked — must be a distinct hue (not red)")
	}
}
