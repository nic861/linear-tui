package tui

import (
	"sort"
	"strings"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// Group-key prefixes (the \x00 separator can't appear in Linear names).
const (
	noProjectLabel   = "(No project)"
	noMilestoneLabel = "(No milestone)"
)

// projectGroupKey / milestoneGroupKey build stable collapse-state keys.
func projectGroupKey(project string) string {
	return "P\x00" + project
}

func milestoneGroupKey(project, milestone string) string {
	return "M\x00" + project + "\x00" + milestone
}

// parseProjectKey returns the project name if key is a project group key.
func parseProjectKey(key string) (project string, ok bool) {
	if strings.HasPrefix(key, "P\x00") {
		return key[len("P\x00"):], true
	}
	return "", false
}

// parseMilestoneKey returns the project + milestone if key is a milestone group key.
func parseMilestoneKey(key string) (project, milestone string, ok bool) {
	if strings.HasPrefix(key, "M\x00") {
		parts := strings.SplitN(key[len("M\x00"):], "\x00", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

// isClosed reports whether an issue counts as resolved for rollup purposes.
func isClosed(stateType string) bool {
	return stateType == "completed" || stateType == "canceled" || stateType == "duplicate"
}

// computeMilestoneSeq returns the dependency depth (1-based) of each issue within
// a single milestone group, considering only blockedBy edges that point at another
// issue in the same group. Issues with no in-group blockers are rank 1; an issue's
// rank is one more than the deepest blocker it waits on. Cycles are broken defensively.
func computeMilestoneSeq(group []*linearapi.Issue) map[string]int {
	byID := make(map[string]*linearapi.Issue, len(group))
	for _, is := range group {
		byID[is.ID] = is
	}

	rank := make(map[string]int, len(group))
	visiting := make(map[string]bool, len(group))

	var rec func(is *linearapi.Issue) int
	rec = func(is *linearapi.Issue) int {
		if r, ok := rank[is.ID]; ok {
			return r
		}
		if visiting[is.ID] {
			return 1 // dependency cycle — treat as a root to avoid infinite recursion
		}
		visiting[is.ID] = true
		maxPred := 0
		for _, b := range is.BlockedBy {
			pred, ok := byID[b.ID]
			if !ok {
				continue // blocker is outside this milestone group
			}
			if pr := rec(pred); pr > maxPred {
				maxPred = pr
			}
		}
		visiting[is.ID] = false
		r := maxPred + 1
		rank[is.ID] = r
		return r
	}

	for _, is := range group {
		rec(is)
	}
	return rank
}

// groupRollup is the aggregate breakdown shown on a collapsed group header.
type groupRollup struct {
	count, done            int
	urgent, high, med, low int
	inProgress, todo       int
	hasCurrentCycle        bool
}

// rollupOf aggregates a group's issues. Priority/state breakdowns count only OPEN
// issues (not done/canceled); done is counted separately for the progress ratio.
func rollupOf(members []*linearapi.Issue) groupRollup {
	var r groupRollup
	for _, is := range members {
		r.count++
		if is.InCurrentCycle {
			r.hasCurrentCycle = true
		}
		if is.StateType == "completed" {
			r.done++
		}
		if isClosed(is.StateType) {
			continue
		}
		switch is.Priority {
		case 1:
			r.urgent++
		case 2:
			r.high++
		case 3:
			r.med++
		default:
			r.low++ // priority 4 (Low) and 0 (None)
		}
		if is.StateType == "started" {
			r.inProgress++
		} else {
			r.todo++ // backlog / unstarted
		}
	}
	return r
}

// BuildGroupedRows produces the milestone-grouped row model: a Project header,
// then a Milestone header per milestone, then the issues in that milestone ordered
// by dependency depth (Seq) and priority. Group headers are collapsible via
// collapsedGroups (keyed by GroupKey). idToIssue maps issue IDs for lookup.
//
// Issues are assumed pre-sorted by project -> milestone (see SortByMilestone), but
// grouping is order-robust: projects and milestones appear in first-seen order.
func BuildGroupedRows(issues []linearapi.Issue, collapsedGroups map[string]bool) ([]IssueRow, map[string]*linearapi.Issue) {
	idToIssue := make(map[string]*linearapi.Issue, len(issues))
	for i := range issues {
		idToIssue[issues[i].ID] = &issues[i]
	}

	// Group into ordered projects -> ordered milestones, preserving first-seen order.
	type projBucket struct {
		name     string
		msOrder  []string
		msIssues map[string][]*linearapi.Issue
	}
	var projOrder []string
	projMap := make(map[string]*projBucket)

	for i := range issues {
		is := &issues[i]
		pName := is.ProjectName
		if pName == "" {
			pName = noProjectLabel
		}
		mName := is.MilestoneName
		if mName == "" {
			mName = noMilestoneLabel
		}
		pb, ok := projMap[pName]
		if !ok {
			pb = &projBucket{name: pName, msIssues: make(map[string][]*linearapi.Issue)}
			projMap[pName] = pb
			projOrder = append(projOrder, pName)
		}
		if _, ok := pb.msIssues[mName]; !ok {
			pb.msOrder = append(pb.msOrder, mName)
		}
		pb.msIssues[mName] = append(pb.msIssues[mName], is)
	}

	var rows []IssueRow

	for _, pName := range projOrder {
		pb := projMap[pName]

		// Project rollup over all its issues.
		var pMembers []*linearapi.Issue
		for _, ms := range pb.msOrder {
			pMembers = append(pMembers, pb.msIssues[ms]...)
		}
		pr := rollupOf(pMembers)
		pKey := projectGroupKey(pName)
		pCollapsed := collapsedGroups[pKey]
		rows = append(rows, IssueRow{
			Kind:            RowProject,
			GroupKey:        pKey,
			GroupLabel:      pName,
			GroupCount:      pr.count,
			GroupDone:       pr.done,
			Collapsed:       pCollapsed,
			OpenUrgent:      pr.urgent,
			OpenHigh:        pr.high,
			OpenMed:         pr.med,
			OpenLow:         pr.low,
			OpenInProgress:  pr.inProgress,
			OpenTodo:        pr.todo,
			HasCurrentCycle: pr.hasCurrentCycle,
		})
		if pCollapsed {
			continue
		}

		for _, mName := range pb.msOrder {
			group := pb.msIssues[mName]
			rank := computeMilestoneSeq(group)

			// Order within the milestone: dependency rank, then priority, then identifier.
			ordered := make([]*linearapi.Issue, len(group))
			copy(ordered, group)
			sort.SliceStable(ordered, func(i, j int) bool {
				ri, rj := rank[ordered[i].ID], rank[ordered[j].ID]
				if ri != rj {
					return ri < rj
				}
				pi, pj := priorityOrder(ordered[i].Priority), priorityOrder(ordered[j].Priority)
				if pi != pj {
					return pi < pj
				}
				return ordered[i].Identifier < ordered[j].Identifier
			})

			// Count rank occurrences to flag parallel (shared-rank) issues.
			rankCount := make(map[int]int, len(ordered))
			for _, is := range ordered {
				rankCount[rank[is.ID]]++
			}

			mr := rollupOf(group)
			mKey := milestoneGroupKey(pName, mName)
			mCollapsed := collapsedGroups[mKey]
			rows = append(rows, IssueRow{
				Kind:            RowMilestone,
				GroupKey:        mKey,
				GroupLabel:      mName,
				GroupCount:      mr.count,
				GroupDone:       mr.done,
				Collapsed:       mCollapsed,
				OpenUrgent:      mr.urgent,
				OpenHigh:        mr.high,
				OpenMed:         mr.med,
				OpenLow:         mr.low,
				OpenInProgress:  mr.inProgress,
				OpenTodo:        mr.todo,
				HasCurrentCycle: mr.hasCurrentCycle,
			})
			if mCollapsed {
				continue
			}

			// Does this milestone have any internal dependency edges? If not, Seq is
			// not meaningful and we suppress the numbers (show a flat marker instead).
			hasDeps := false
			groupIDs := make(map[string]bool, len(group))
			for _, is := range group {
				groupIDs[is.ID] = true
			}
			for _, is := range group {
				for _, b := range is.BlockedBy {
					if groupIDs[b.ID] {
						hasDeps = true
						break
					}
				}
				if hasDeps {
					break
				}
			}

			for _, is := range ordered {
				r := IssueRow{
					Kind:    RowIssue,
					IssueID: is.ID,
				}
				if hasDeps {
					r.Seq = rank[is.ID]
					r.SeqParallel = rankCount[rank[is.ID]] > 1
				}
				rows = append(rows, r)
			}
		}
	}

	return rows, idToIssue
}

// seqLabel renders the Seq cell text for an issue row.
func seqLabel(r IssueRow) string {
	if r.Seq <= 0 {
		return "·"
	}
	if r.SeqParallel {
		return itoa(r.Seq) + "∥"
	}
	return itoa(r.Seq)
}

// itoa is a tiny strconv.Itoa to avoid an extra import churn at call sites.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
