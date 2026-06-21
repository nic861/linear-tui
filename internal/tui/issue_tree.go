package tui

import (
	"sort"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// RowKind distinguishes issue rows from group-header rows in the grouped view.
type RowKind int

const (
	RowIssue     RowKind = iota // a normal issue row
	RowProject                  // a project group-header row
	RowMilestone                // a milestone group-header row
	RowLabel                    // a label group-header row (group-by-label view)
)

// IssueRow represents a single row in the issues table with hierarchy info.
// A row is either an issue (Kind == RowIssue) or a collapsible group header
// (Kind == RowProject / RowMilestone) used by the milestone-grouped view.
type IssueRow struct {
	Kind        RowKind // issue vs group header
	IssueID     string  // Reference to the issue (RowIssue only)
	Level       int     // Nesting level (0 = top-level, 1 = child, etc.)
	IsParent    bool    // True if this issue has children
	HasChildren bool    // True if this issue has children (same as IsParent for now)
	IsExpanded  bool    // True if children are shown (only meaningful when HasChildren is true)

	// Group-header fields (RowProject / RowMilestone only).
	GroupKey   string // stable key for collapse state
	GroupLabel string // display name (project or milestone name)
	GroupCount int    // number of issues in the group
	GroupDone  int    // number of completed issues in the group
	Collapsed  bool   // whether the group is collapsed

	// Group-header rollup (open = not done/canceled), for scanning collapsed lists.
	OpenUrgent      int  // priority 1
	OpenHigh        int  // priority 2
	OpenMed         int  // priority 3
	OpenLow         int  // priority 4 (and no-priority)
	OpenInProgress  int  // started state
	OpenTodo        int  // backlog/unstarted
	HasCurrentCycle bool // any issue in the group is in the active cycle

	// Sequence fields (RowIssue in grouped/milestone mode only).
	Seq         int  // dependency depth within the milestone (0 = not applicable)
	SeqParallel bool // shares its rank with a sibling (no ordering constraint between them)
}

// IsGroupHeader reports whether the row is a collapsible group header
// (project, milestone, or label) rather than an issue.
func (r IssueRow) IsGroupHeader() bool {
	return r.Kind == RowProject || r.Kind == RowMilestone || r.Kind == RowLabel
}

// BuildIssueRows constructs a flattened list of rows for table rendering.
// It builds a hierarchical view where parent issues can be expanded/collapsed.
// Returns the rows and a map for quick issue lookup by ID.
func BuildIssueRows(issues []linearapi.Issue, expanded map[string]bool) ([]IssueRow, map[string]*linearapi.Issue) {
	idToIssue := make(map[string]*linearapi.Issue, len(issues))
	for i := range issues {
		idToIssue[issues[i].ID] = &issues[i]
	}

	// Separate parent issues (no parent in our list) from children
	// An issue is a "top-level" issue if:
	// 1. It has no parent (issue.Parent == nil), OR
	// 2. Its parent is not in our fetched list (orphan sub-issue)
	var topLevel []*linearapi.Issue
	childrenByParent := make(map[string][]*linearapi.Issue)

	for i := range issues {
		issue := &issues[i]
		if issue.Parent == nil {
			// No parent - this is a top-level issue
			topLevel = append(topLevel, issue)
		} else if _, parentInList := idToIssue[issue.Parent.ID]; parentInList {
			// Parent is in our list - group under parent
			childrenByParent[issue.Parent.ID] = append(childrenByParent[issue.Parent.ID], issue)
		} else {
			// Orphan sub-issue (parent not in list) - treat as top-level with marker
			topLevel = append(topLevel, issue)
		}
	}

	// Build rows
	var rows []IssueRow

	for _, issue := range topLevel {
		// Check if this issue has children in our list
		children := childrenByParent[issue.ID]
		hasChildren := len(children) > 0 || len(issue.Children) > 0
		isExpanded := expanded[issue.ID]

		rows = append(rows, IssueRow{
			IssueID:     issue.ID,
			Level:       0,
			IsParent:    hasChildren,
			HasChildren: hasChildren,
			IsExpanded:  isExpanded,
		})

		// If expanded, add children
		if hasChildren && isExpanded {
			// Use children from our fetched list if available
			if len(children) > 0 {
				// Sort children by identifier for consistent ordering
				sort.Slice(children, func(i, j int) bool {
					return children[i].Identifier < children[j].Identifier
				})

				for _, child := range children {
					childHasChildren := len(child.Children) > 0
					childExpanded := expanded[child.ID]

					rows = append(rows, IssueRow{
						IssueID:     child.ID,
						Level:       1,
						IsParent:    childHasChildren,
						HasChildren: childHasChildren,
						IsExpanded:  childExpanded,
					})
				}
			}
		}
	}

	return rows, idToIssue
}

// ToggleExpanded toggles the expanded state for an issue.
// Returns the new expanded state.
func ToggleExpanded(expanded map[string]bool, issueID string) bool {
	newState := !expanded[issueID]
	expanded[issueID] = newState
	return newState
}

// CollapseAll sets all issues to collapsed state.
func CollapseAll(expanded map[string]bool) {
	for k := range expanded {
		delete(expanded, k)
	}
}

// ExpandAll expands all parent issues.
func ExpandAll(expanded map[string]bool, issues []linearapi.Issue) {
	for _, issue := range issues {
		if len(issue.Children) > 0 || issue.Parent == nil {
			// Expand issues that have children
			expanded[issue.ID] = true
		}
	}
}
