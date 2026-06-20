package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// markdownRenderer is a shared glamour renderer for markdown content.
var markdownRenderer *glamour.TermRenderer

// initMarkdownRenderer initializes the glamour markdown renderer.
func initMarkdownRenderer() {
	var err error
	markdownRenderer, err = glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		// Fallback: create a basic renderer if custom style fails
		markdownRenderer, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(80),
		)
	}
}

// renderMarkdown renders markdown content using glamour.
// Falls back to plain text if rendering fails.
func renderMarkdown(content string) string {
	if markdownRenderer == nil {
		initMarkdownRenderer()
	}

	rendered, err := markdownRenderer.Render(content)
	if err != nil {
		// Fallback to plain text on error
		return content
	}

	// Trim extra whitespace that glamour may add
	return strings.TrimSpace(rendered)
}

// buildDetailsView creates and configures the details view with separate description and comments sections.
func (a *App) buildDetailsView() *tview.Flex {
	// Create description/metadata view (top section, scrollable)
	a.detailsDescriptionView = tview.NewTextView()
	a.detailsDescriptionView.SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true).
		SetBorder(true).
		SetTitle(" Details ").
		SetTitleColor(a.theme.Foreground).
		SetBorderColor(a.theme.Border).
		SetBackgroundColor(tcell.ColorDefault)
	padding := a.density.DetailsPadding
	a.detailsDescriptionView.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	// Create comments view (bottom section, scrollable, fixed height)
	a.detailsCommentsView = tview.NewTextView()
	a.detailsCommentsView.SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true).
		SetBorder(true).
		SetTitle(" Comments ").
		SetTitleColor(a.theme.Foreground).
		SetBorderColor(a.theme.Border).
		SetBackgroundColor(tcell.ColorDefault)
	a.detailsCommentsView.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	// Create flex layout; comments are added conditionally after issue selection.
	detailsFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	a.detailsView = detailsFlex
	a.setDetailsCommentsVisibility(false)

	return a.detailsView
}

// setDetailsCommentsVisibility rebuilds the details layout to show or hide comments.
func (a *App) setDetailsCommentsVisibility(showComments bool) {
	if a.detailsView == nil || a.detailsDescriptionView == nil || a.detailsCommentsView == nil {
		return
	}
	if a.detailsCommentsVisible == showComments && a.detailsView.GetItemCount() > 0 {
		return
	}

	a.detailsView.Clear().
		AddItem(a.detailsDescriptionView, 0, 3, true)
	if showComments {
		a.detailsView.AddItem(a.detailsCommentsView, 0, 2, false)
	}

	a.detailsCommentsVisible = showComments
	if !showComments {
		a.focusedDetailsView = false
	}
}

func buildDetailsHeader(issue *linearapi.Issue, tags ThemeTags, sectionGap int) []string {
	keyColor := tags.SecondaryText
	valColor := tags.Foreground
	accentColor := tags.Accent
	dividerColor := tags.Border

	var headerLines []string

	// Issue header info with styling
	headerLines = append(headerLines, fmt.Sprintf("%s%s[-]", accentColor, issue.Identifier))
	headerLines = append(headerLines, fmt.Sprintf("[b]%s%s[-]", valColor, issue.Title))
	for i := 0; i < sectionGap; i++ {
		headerLines = append(headerLines, "")
	}

	// Metadata grid simulation
	headerLines = append(headerLines, fmt.Sprintf("%sState:[-]      %s%s[-]", keyColor, valColor, issue.State))

	assignee := "Unassigned"
	if issue.Assignee != "" {
		assignee = issue.Assignee
	}
	headerLines = append(headerLines, fmt.Sprintf("%sAssignee:[-]   %s%s[-]", keyColor, valColor, assignee))

	headerLines = append(headerLines, fmt.Sprintf("%sPriority:[-]   %s%d[-]", keyColor, valColor, issue.Priority))

	// Project
	if issue.ProjectName != "" {
		headerLines = append(headerLines, fmt.Sprintf("%sProject:[-]    %s%s[-]", keyColor, valColor, issue.ProjectName))
	}

	// Milestone
	if issue.MilestoneName != "" {
		headerLines = append(headerLines, fmt.Sprintf("%sMilestone:[-]  %s%s[-]", keyColor, accentColor, issue.MilestoneName))
	}

	// Cycle
	if issue.CycleName != "" {
		headerLines = append(headerLines, fmt.Sprintf("%sCycle:[-]      %s%s[-]", keyColor, valColor, issue.CycleName))
	}

	// Labels
	labelsText := "No labels"
	if len(issue.Labels) > 0 {
		labelNames := make([]string, len(issue.Labels))
		for i, lbl := range issue.Labels {
			labelNames[i] = lbl.Name
		}
		labelsText = strings.Join(labelNames, ", ")
	}
	headerLines = append(headerLines, fmt.Sprintf("%sLabels:[-]     %s%s[-]", keyColor, valColor, labelsText))

	// Parent issue (if this is a sub-issue)
	if issue.Parent != nil {
		parentText := fmt.Sprintf("%s - %s", issue.Parent.Identifier, issue.Parent.Title)
		headerLines = append(headerLines, fmt.Sprintf("%sParent:[-]     %s%s[-]", keyColor, accentColor, parentText))
	}

	// Sub-issues (if this is a parent issue)
	if len(issue.Children) > 0 {
		for i := 0; i < sectionGap; i++ {
			headerLines = append(headerLines, "")
		}
		headerLines = append(headerLines, fmt.Sprintf("%sSub-issues:[-] %s%d items[-]", keyColor, valColor, len(issue.Children)))
		for _, child := range issue.Children {
			childLine := fmt.Sprintf("  %s└─[-] %s%s[-] %s[%s][-] %s%s[-]",
				keyColor,
				accentColor, child.Identifier,
				keyColor, child.State,
				valColor, child.Title)
			headerLines = append(headerLines, childLine)
		}
	}

	if len(issue.BlockedBy) > 0 {
		headerLines = appendRelationSection(headerLines, "Blocked by:", issue.BlockedBy, keyColor, valColor, accentColor, sectionGap)
	}
	if len(issue.Blocks) > 0 {
		headerLines = appendRelationSection(headerLines, "Blocks:", issue.Blocks, keyColor, valColor, accentColor, sectionGap)
	}

	for i := 0; i < sectionGap; i++ {
		headerLines = append(headerLines, "")
	}
	headerLines = append(headerLines, fmt.Sprintf("%s────────────────────────────────────────[-]", dividerColor))
	for i := 0; i < sectionGap; i++ {
		headerLines = append(headerLines, "")
	}

	return headerLines
}

func appendRelationSection(
	headerLines []string,
	label string,
	relations []linearapi.IssueRelationRef,
	keyColor string,
	valColor string,
	accentColor string,
	sectionGap int,
) []string {
	for i := 0; i < sectionGap; i++ {
		headerLines = append(headerLines, "")
	}
	padding := strings.Repeat(" ", 12-len(label))
	headerLines = append(headerLines, fmt.Sprintf("%s%s[-]%s%s%d items[-]", keyColor, label, padding, valColor, len(relations)))
	for _, relation := range relations {
		relationLine := fmt.Sprintf("  %s└─[-] %s%s[-] %s[%s][-] %s%s[-]",
			keyColor,
			accentColor, relation.Identifier,
			keyColor, relation.State,
			valColor, relation.Title)
		headerLines = append(headerLines, relationLine)
	}

	return headerLines
}

// showGroupDetails renders a project/milestone group panel into the details view
// (a milestone-grouped-view affordance: select a header row to inspect the group).
func (a *App) showGroupDetails(r IssueRow) {
	if a.detailsDescriptionView == nil {
		return
	}
	a.issuesMu.Lock()
	a.selectedIssue = nil
	issues := a.issues
	a.issuesMu.Unlock()

	a.setDetailsCommentsVisibility(false)
	a.detailsDescriptionView.Clear()
	a.detailsDescriptionView.SetText(buildGroupDetailsText(r, issues, a.themeTags))
	a.detailsDescriptionView.ScrollToBeginning()
}

// truncateRunes shortens s to at most n runes, appending an ellipsis when cut.
func truncateRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n <= 1 {
		return string(rs[:n])
	}
	return string(rs[:n-1]) + "…"
}

// buildGroupDetailsText renders the right-pane content for a project/milestone header.
func buildGroupDetailsText(r IssueRow, issues []linearapi.Issue, tags ThemeTags) string {
	keyColor := tags.SecondaryText
	valColor := tags.Foreground
	accentColor := tags.Accent

	var titleLine, projName, msName string
	isMilestone := false
	scope := func(linearapi.Issue) bool { return false }

	if p, ok := parseProjectKey(r.GroupKey); ok {
		projName = p
		titleLine = fmt.Sprintf("%s▦ %s[-]", accentColor, p)
		scope = func(is linearapi.Issue) bool {
			pn := is.ProjectName
			if pn == "" {
				pn = noProjectLabel
			}
			return pn == p
		}
	} else if p, m, ok := parseMilestoneKey(r.GroupKey); ok {
		isMilestone = true
		projName, msName = p, m
		titleLine = fmt.Sprintf("%s◈ %s[-]", accentColor, m)
		scope = func(is linearapi.Issue) bool {
			pn := is.ProjectName
			if pn == "" {
				pn = noProjectLabel
			}
			mn := is.MilestoneName
			if mn == "" {
				mn = noMilestoneLabel
			}
			return pn == p && mn == m
		}
	} else {
		return ""
	}
	_ = msName

	var members []*linearapi.Issue
	var done, prog, todo, canc int
	for i := range issues {
		if !scope(issues[i]) {
			continue
		}
		members = append(members, &issues[i])
		switch issues[i].StateType {
		case "completed":
			done++
		case "started":
			prog++
		case "canceled", "duplicate":
			canc++
		default:
			todo++
		}
	}
	total := len(members)

	var b strings.Builder
	b.WriteString(titleLine + "\n\n")
	if isMilestone {
		b.WriteString(fmt.Sprintf("%sProject:[-]    %s%s[-]\n", keyColor, valColor, projName))
	}
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}
	b.WriteString(fmt.Sprintf("%sProgress:[-]   %s%d%%[-]  %s%s[-]\n", keyColor, valColor, pct, accentColor, progressBar(done, total)))
	b.WriteString(fmt.Sprintf("%sIssues:[-]     %s● %d done  ◐ %d in progress  ○ %d todo", keyColor, valColor, done, prog, todo))
	if canc > 0 {
		b.WriteString(fmt.Sprintf("  ✕ %d", canc))
	}
	b.WriteString(fmt.Sprintf("  (%d total)[-]\n", total))

	if isMilestone {
		ids := make(map[string]bool, len(members))
		doneSet := make(map[string]bool, len(members))
		for _, m := range members {
			ids[m.ID] = true
			if isClosed(m.StateType) {
				doneSet[m.ID] = true
			}
		}
		var ready []*linearapi.Issue
		for _, m := range members {
			if isClosed(m.StateType) {
				continue
			}
			blocked := false
			for _, bl := range m.BlockedBy {
				if ids[bl.ID] && !doneSet[bl.ID] {
					blocked = true
					break
				}
			}
			if !blocked {
				ready = append(ready, m)
			}
		}
		b.WriteString("\n")
		switch {
		case total > 0 && done == total:
			b.WriteString(fmt.Sprintf("%sNext up:[-]    %sall complete ✓[-]\n", keyColor, accentColor))
		case len(ready) == 0:
			b.WriteString(fmt.Sprintf("%sNext up:[-]    %snone unblocked[-]\n", keyColor, keyColor))
		default:
			b.WriteString(fmt.Sprintf("%sNext up:[-]\n", keyColor))
			for i, m := range ready {
				if i >= 6 {
					b.WriteString(fmt.Sprintf("  %s… +%d more[-]\n", keyColor, len(ready)-6))
					break
				}
				b.WriteString(fmt.Sprintf("  %s%s[-] %s%s[-]\n", accentColor, m.Identifier, valColor, truncateRunes(m.Title, 52)))
			}
		}
	} else {
		seen := make(map[string]bool)
		var msList []string
		for _, m := range members {
			mn := m.MilestoneName
			if mn == "" {
				mn = noMilestoneLabel
			}
			if !seen[mn] {
				seen[mn] = true
				msList = append(msList, mn)
			}
		}
		b.WriteString(fmt.Sprintf("\n%sMilestones:[-] %s%s[-]\n", keyColor, valColor, strings.Join(msList, ", ")))
	}

	return b.String()
}

// updateDetailsView updates the details view with the selected issue.
func (a *App) updateDetailsView() {
	a.issuesMu.RLock()
	selectedIssue := a.selectedIssue
	a.issuesMu.RUnlock()
	hasComments := selectedIssue != nil && len(selectedIssue.Comments) > 0
	a.setDetailsCommentsVisibility(hasComments)
	if selectedIssue == nil {
		a.detailsDescriptionView.SetText(fmt.Sprintf("%sNo issue selected. Select an issue from the list to view details.[-]", a.themeTags.SecondaryText))
		a.detailsCommentsView.SetText("")
		if a.focusedPane == FocusDetails && !a.detailsCommentsVisible {
			a.updateFocus()
		}
		return
	}

	issue := selectedIssue

	keyColor := a.themeTags.SecondaryText
	accentColor := a.themeTags.Accent
	dividerColor := a.themeTags.Border

	// ===== Update Description/Metadata View =====
	headerLines := buildDetailsHeader(issue, a.themeTags, a.density.DetailsSectionGap)

	// Set header first, then append description via ANSIWriter
	a.detailsDescriptionView.Clear()
	a.detailsDescriptionView.SetText(strings.Join(headerLines, "\n"))
	writer := tview.ANSIWriter(a.detailsDescriptionView)

	// Description
	if issue.Description != "" {
		_, _ = fmt.Fprintf(writer, "%sDescription:[-]\n\n", keyColor)

		// Render description as markdown and write through ANSIWriter
		// ANSIWriter translates ANSI escape codes to tview color tags
		renderedDesc := renderMarkdown(issue.Description)
		_, _ = fmt.Fprint(writer, renderedDesc)
	} else {
		_, _ = fmt.Fprintf(writer, "%sNo description available[-]", keyColor)
	}

	a.detailsDescriptionView.ScrollToBeginning()

	// ===== Update Comments View =====
	a.detailsCommentsView.Clear()
	commentsWriter := tview.ANSIWriter(a.detailsCommentsView)

	if len(issue.Comments) > 0 {
		_, _ = fmt.Fprintf(commentsWriter, "%sComments:[-] (%d)\n\n", keyColor, len(issue.Comments))

		for i, comment := range issue.Comments {
			// Comment header: author and timestamp
			authorDisplay := comment.Author.DisplayName
			if authorDisplay == "" {
				authorDisplay = comment.Author.Name
			}
			if comment.Author.IsMe {
				authorDisplay = fmt.Sprintf("%s (me)", authorDisplay)
			}

			// Format timestamp
			timeStr := comment.CreatedAt.Format("Jan 2, 2006 3:04 PM")
			if !comment.UpdatedAt.Equal(comment.CreatedAt) {
				timeStr += " (edited)"
			}

			_, _ = fmt.Fprintf(commentsWriter, "%s%s[-] %s%s[-]\n", accentColor, authorDisplay, keyColor, timeStr)
			_, _ = fmt.Fprint(commentsWriter, "\n")

			// Render comment body as markdown
			renderedComment := renderMarkdown(comment.Body)
			_, _ = fmt.Fprint(commentsWriter, renderedComment)

			// Add separator between comments (but not after the last one)
			if i < len(issue.Comments)-1 {
				_, _ = fmt.Fprint(commentsWriter, "\n\n")
				_, _ = fmt.Fprintf(commentsWriter, "%s────────────────────────────────────────[-]\n\n", dividerColor)
			}
		}
	} else {
		// Empty state for comments
		_, _ = fmt.Fprintf(commentsWriter, "%sNo comments yet.[-]", keyColor)
	}

	a.detailsCommentsView.ScrollToBeginning()
	if a.focusedPane == FocusDetails && !a.detailsCommentsVisible {
		a.updateFocus()
	}
}
