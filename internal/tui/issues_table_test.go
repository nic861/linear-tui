package tui

import (
	"testing"
	"time"

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
