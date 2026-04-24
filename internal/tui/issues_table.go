package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// Tree icons for expand/collapse indicators.
const (
	IconExpanded    = "▼"
	IconCollapsed   = "▶"
	IconChildPrefix = "└─"
)

// formatPriority formats a priority value into a display string with icon and label.
// Linear priority: 0 = No priority, 1 = Urgent, 2 = High, 3 = Normal, 4 = Low.
func formatPriority(priority int, theme Theme) (string, tcell.Color) {
	switch priority {
	case 1:
		return Icons.Priority + " Urgent", theme.StatusCanceled // Red for urgent
	case 2:
		return Icons.Priority + " High", theme.StatusInProgress // Yellow for high
	case 3:
		return Icons.Priority + " Normal", theme.Foreground // Default for normal
	case 4:
		return Icons.Priority + " Low", theme.SecondaryText // Gray for low
	default:
		return "-", theme.SecondaryText // No priority
	}
}

func isBlocked(issue *linearapi.Issue) bool {
	if issue == nil {
		return false
	}
	for _, blocker := range issue.BlockedBy {
		if blocker.StateType != "completed" && blocker.StateType != "canceled" {
			return true
		}
	}
	return false
}

func renderIdentifierCell(issue *linearapi.Issue, theme Theme, issueRow IssueRow) *tview.TableCell {
	identifierPrefix := " "
	if issueRow.Level > 0 {
		identifierPrefix = " " + IconChildPrefix + " "
	} else if issueRow.HasChildren {
		if issueRow.IsExpanded {
			identifierPrefix = " " + IconExpanded + " "
		} else {
			identifierPrefix = " " + IconCollapsed + " "
		}
	}

	textColor := theme.SecondaryText
	if isBlocked(issue) {
		textColor = theme.StatusBlocked
	}

	cell := tview.NewTableCell(identifierPrefix + issue.Identifier).
		SetAlign(tview.AlignLeft)
	cell.Color = textColor
	return cell
}

// getIssueFromRow returns the issue for a given table row (accounting for header).
// Returns nil if the row is invalid.
func (a *App) getIssueFromRow(row int) *linearapi.Issue {
	return getIssueFromRowModel(row, a.issueRows, a.idToIssue)
}

// getRowForIssue returns the table row for a given issue ID.
// Returns -1 if not found.
func (a *App) getRowForIssue(issueID string) int {
	return getRowForIssueModel(issueID, a.issueRows)
}

// getIssueFromRowModel returns the issue for a given table row using the provided model.
// Returns nil if the row is invalid.
func getIssueFromRowModel(row int, rows []IssueRow, idToIssue map[string]*linearapi.Issue) *linearapi.Issue {
	rowIndex := row - 1 // Account for header row
	if rowIndex < 0 || rowIndex >= len(rows) {
		return nil
	}
	issueID := rows[rowIndex].IssueID
	if issue, ok := idToIssue[issueID]; ok {
		return issue
	}
	return nil
}

// getRowForIssueModel returns the table row for a given issue ID using the provided model.
// Returns -1 if not found.
func getRowForIssueModel(issueID string, rows []IssueRow) int {
	for i, row := range rows {
		if row.IssueID == issueID {
			return i + 1 // +1 for header row
		}
	}
	return -1
}

// buildIssuesTable creates and configures the issues table widget.
func (a *App) buildIssuesTable(title string) *tview.Table {
	table := tview.NewTable()
	table.SetBorders(false).
		SetSelectable(true, false).
		SetBorder(true).
		SetTitle(title).
		SetTitleColor(a.theme.Foreground).
		SetBorderColor(a.theme.Border).
		SetBackgroundColor(a.theme.Background)

	table.SetSelectedStyle(tcell.StyleDefault.
		Foreground(a.theme.SelectionText).
		Background(a.theme.SelectionBg).
		Bold(true))

	// Set column headers with better styling
	headerStyle := tcell.StyleDefault.
		Foreground(a.theme.HeaderText).
		Background(a.theme.HeaderBg).
		Bold(true)

	table.SetCell(0, 0, tview.NewTableCell(" ID").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 1, tview.NewTableCell("Cycle").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 2, tview.NewTableCell("State").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 3, tview.NewTableCell("Priority").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 4, tview.NewTableCell("Project").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(2))
	table.SetCell(0, 5, tview.NewTableCell("Title").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(6))

	table.SetFixed(1, 0)

	// Handle selection (Enter to open details or toggle expand)
	table.SetSelectedFunc(func(row, _ int) {
		issue := a.getIssueFromRow(row)
		if issue == nil {
			return
		}

		// If issue has children, toggle expand/collapse
		if len(issue.Children) > 0 {
			a.toggleIssueExpanded(issue.ID)
			return
		}

		// Otherwise, focus on details
		a.onIssueSelected(*issue)
		a.focusedPane = FocusDetails
		a.updateFocus()
	})

	// Set up keyboard navigation
	a.setupIssuesTableNavigation(table)

	return table
}

// setupIssuesTableNavigation sets up keyboard navigation for the issues table.
func (a *App) setupIssuesTableNavigation(table *tview.Table) {
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'j':
				row, _ := table.GetSelection()
				if row < table.GetRowCount()-1 {
					table.Select(row+1, 0)
					if issue := a.getIssueFromRow(row + 1); issue != nil {
						a.onIssueSelected(*issue)
					}
				}
				return nil
			case 'k':
				row, _ := table.GetSelection()
				if row > 1 {
					table.Select(row-1, 0)
					if issue := a.getIssueFromRow(row - 1); issue != nil {
						a.onIssueSelected(*issue)
					}
				}
				return nil
			case 'g':
				table.Select(1, 0)
				if issue := a.getIssueFromRow(1); issue != nil {
					a.onIssueSelected(*issue)
				}
				return nil
			case 'G':
				if len(a.issueRows) > 0 {
					lastRow := len(a.issueRows)
					table.Select(lastRow, 0)
					if issue := a.getIssueFromRow(lastRow); issue != nil {
						a.onIssueSelected(*issue)
					}
				}
				return nil
			case 'l':
				// Expand current parent issue
				row, _ := table.GetSelection()
				if issue := a.getIssueFromRow(row); issue != nil {
					if len(issue.Children) > 0 && !a.expandedState[issue.ID] {
						a.toggleIssueExpanded(issue.ID)
					}
				}
				return nil
			case 'h':
				// Collapse current parent issue, or go to parent if on child
				row, _ := table.GetSelection()
				if issue := a.getIssueFromRow(row); issue != nil {
					if len(issue.Children) > 0 && a.expandedState[issue.ID] {
						a.toggleIssueExpanded(issue.ID)
					} else if issue.Parent != nil {
						parentRow := a.getRowForIssue(issue.Parent.ID)
						if parentRow > 0 {
							table.Select(parentRow, 0)
							if parent := a.getIssueFromRow(parentRow); parent != nil {
								a.onIssueSelected(*parent)
							}
						}
					}
				}
				return nil
			case ' ':
				// Space toggles expand/collapse
				row, _ := table.GetSelection()
				if issue := a.getIssueFromRow(row); issue != nil {
					if len(issue.Children) > 0 {
						a.toggleIssueExpanded(issue.ID)
					}
				}
				return nil
			}
		case tcell.KeyEnter:
			row, _ := table.GetSelection()
			issue := a.getIssueFromRow(row)
			if issue == nil {
				return nil
			}

			if len(issue.Children) > 0 {
				a.toggleIssueExpanded(issue.ID)
				return nil
			}

			a.onIssueSelected(*issue)
			a.focusedPane = FocusDetails
			a.updateFocus()
			return nil
		case tcell.KeyDown:
			row, _ := table.GetSelection()
			if row < table.GetRowCount()-1 {
				table.Select(row+1, 0)
				if issue := a.getIssueFromRow(row + 1); issue != nil {
					a.onIssueSelected(*issue)
				}
			}
			return nil
		case tcell.KeyUp:
			row, _ := table.GetSelection()
			if row > 1 {
				table.Select(row-1, 0)
				if issue := a.getIssueFromRow(row - 1); issue != nil {
					a.onIssueSelected(*issue)
				}
			}
			return nil
		}
		return event
	})
}

// renderIssuesTableModel renders a table with the given rows and issue lookup map.
func renderIssuesTableModel(table *tview.Table, rows []IssueRow, idToIssue map[string]*linearapi.Issue, selectedIssueID string, theme Theme) {
	table.Clear()

	// Set column headers with better styling
	headerStyle := tcell.StyleDefault.
		Foreground(theme.HeaderText).
		Background(theme.HeaderBg).
		Bold(true)

	table.SetCell(0, 0, tview.NewTableCell(" ID").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 1, tview.NewTableCell("Cycle").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 2, tview.NewTableCell("State").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 3, tview.NewTableCell("Priority").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 4, tview.NewTableCell("Project").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(2))
	table.SetCell(0, 5, tview.NewTableCell("Title").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(6))

	// Add issue rows using the hierarchical structure
	for i, issueRow := range rows {
		row := i + 1

		issue, ok := idToIssue[issueRow.IssueID]
		if !ok || issue == nil {
			continue
		}

		table.SetCell(row, 0, renderIdentifierCell(issue, theme, issueRow))

		// Cycle
		cycleName := issue.CycleName
		cycleColor := theme.Foreground
		if cycleName == "" {
			cycleName = "-"
			cycleColor = theme.SecondaryText
		}
		if len(cycleName) > 16 {
			cycleName = cycleName[:16]
		}
		table.SetCell(row, 1, tview.NewTableCell(cycleName).
			SetTextColor(cycleColor).
			SetAlign(tview.AlignLeft))

		// State with color based on state
		state := issue.State
		var stateColor tcell.Color
		var stateIcon string

		lowerState := strings.ToLower(state)
		switch {
		case strings.Contains(lowerState, "done") || strings.Contains(lowerState, "complete"):
			stateColor = theme.StatusDone
			stateIcon = Icons.Done
		case strings.Contains(lowerState, "progress"):
			stateColor = theme.StatusInProgress
			stateIcon = Icons.InProgress
		case strings.Contains(lowerState, "cancel"):
			stateColor = theme.StatusCanceled
			stateIcon = Icons.Done
		default:
			stateColor = theme.StatusTodo
			stateIcon = Icons.Todo
		}

		if len(state) > 12 {
			state = state[:12]
		}

		table.SetCell(row, 2, tview.NewTableCell(stateIcon+" "+state).
			SetTextColor(stateColor).
			SetAlign(tview.AlignLeft))

		// Priority
		priorityText, priorityColor := formatPriority(issue.Priority, theme)
		table.SetCell(row, 3, tview.NewTableCell(priorityText).
			SetTextColor(priorityColor).
			SetAlign(tview.AlignLeft))

		// Project
		project := issue.ProjectName
		projectColor := theme.Foreground
		if project == "" {
			project = "-"
			projectColor = theme.SecondaryText
		}
		if len(project) > 20 {
			project = project[:20]
		}

		table.SetCell(row, 4, tview.NewTableCell(project).
			SetTextColor(projectColor).
			SetAlign(tview.AlignLeft))

		// Title
		title := issue.Title
		table.SetCell(row, 5, tview.NewTableCell(title).
			SetTextColor(theme.Foreground).
			SetAlign(tview.AlignLeft))
	}

	// Select the specified issue or first row
	if len(rows) > 0 {
		selectedRow := 1
		if selectedIssueID != "" {
			for i, row := range rows {
				if row.IssueID == selectedIssueID {
					selectedRow = i + 1
					break
				}
			}
		}
		table.Select(selectedRow, 0)
	} else {
		table.SetCell(1, 0, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 1, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 2, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 3, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 4, tview.NewTableCell("No issues").
			SetTextColor(theme.SecondaryText).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
		table.SetCell(1, 5, tview.NewTableCell("").SetSelectable(false))
	}
}

// renderIssueRow formats an issue for display in the table.
func renderIssueRow(issue linearapi.Issue) []string {
	identifier := issue.Identifier
	if len(identifier) > 10 {
		identifier = identifier[:10]
	}

	cycleName := issue.CycleName
	if cycleName == "" {
		cycleName = "-"
	}
	if len(cycleName) > 16 {
		cycleName = cycleName[:16]
	}

	state := issue.State
	if len(state) > 10 {
		state = state[:10]
	}

	priorityText, _ := formatPriority(issue.Priority, LinearTheme)

	project := issue.ProjectName
	if project == "" {
		project = "-"
	}
	if len(project) > 20 {
		project = project[:20]
	}

	return []string{identifier, cycleName, state, priorityText, project, issue.Title}
}
