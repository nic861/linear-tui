package tui

import "testing"

import "github.com/roeyazroel/linear-tui/internal/linearapi"

func blk(ids ...string) []linearapi.IssueRelationRef {
	refs := make([]linearapi.IssueRelationRef, len(ids))
	for i, id := range ids {
		refs[i] = linearapi.IssueRelationRef{ID: id}
	}
	return refs
}

// The real Dory v0.9 chain: 390 -> {385,386} -> 387 -> 388 (388 also in v0.9).
// 385 and 386 both depend only on 390, so they share rank 2 (parallel).
func TestComputeMilestoneSeq_DoryChain(t *testing.T) {
	group := []*linearapi.Issue{
		{ID: "390"},
		{ID: "385", BlockedBy: blk("390")},
		{ID: "386", BlockedBy: blk("390")},
		{ID: "387", BlockedBy: blk("385", "386")},
		{ID: "388", BlockedBy: blk("387")},
	}
	rank := computeMilestoneSeq(group)
	want := map[string]int{"390": 1, "385": 2, "386": 2, "387": 3, "388": 4}
	for id, w := range want {
		if rank[id] != w {
			t.Errorf("rank[%s] = %d, want %d", id, rank[id], w)
		}
	}
}

// Blockers outside the group must not affect in-group ranks.
func TestComputeMilestoneSeq_IgnoresOutOfGroup(t *testing.T) {
	group := []*linearapi.Issue{
		{ID: "a", BlockedBy: blk("external-x")},
		{ID: "b", BlockedBy: blk("a")},
	}
	rank := computeMilestoneSeq(group)
	if rank["a"] != 1 {
		t.Errorf("rank[a] = %d, want 1 (external blocker ignored)", rank["a"])
	}
	if rank["b"] != 2 {
		t.Errorf("rank[b] = %d, want 2", rank["b"])
	}
}

// A dependency cycle must not hang and must yield finite ranks.
func TestComputeMilestoneSeq_CycleSafe(t *testing.T) {
	group := []*linearapi.Issue{
		{ID: "x", BlockedBy: blk("y")},
		{ID: "y", BlockedBy: blk("x")},
	}
	rank := computeMilestoneSeq(group)
	if rank["x"] == 0 || rank["y"] == 0 {
		t.Errorf("cycle produced zero rank: x=%d y=%d", rank["x"], rank["y"])
	}
}

func TestBuildGroupedRows_HeadersAndSeq(t *testing.T) {
	issues := []linearapi.Issue{
		{ID: "390", Identifier: "EFF-390", ProjectName: "Dory Q&A", MilestoneName: "v0.9", StateType: "completed"},
		{ID: "385", Identifier: "EFF-385", ProjectName: "Dory Q&A", MilestoneName: "v0.9", StateType: "started", BlockedBy: blk("390")},
		{ID: "386", Identifier: "EFF-386", ProjectName: "Dory Q&A", MilestoneName: "v0.9", StateType: "backlog", BlockedBy: blk("390")},
		// A flat project: no milestone, no deps.
		{ID: "100", Identifier: "EFF-100", ProjectName: "Infra", StateType: "backlog"},
	}
	rows, idx := BuildGroupedRows(issues, map[string]bool{}, map[string]bool{})
	if len(idx) != 4 {
		t.Fatalf("idToIssue size = %d, want 4", len(idx))
	}

	// First row is the Dory project header with rollup 1/3.
	if rows[0].Kind != RowProject || rows[0].GroupLabel != "Dory Q&A" {
		t.Fatalf("row0 = %+v, want Dory project header", rows[0])
	}
	if rows[0].GroupCount != 3 || rows[0].GroupDone != 1 {
		t.Errorf("Dory rollup = %d/%d, want 1/3", rows[0].GroupDone, rows[0].GroupCount)
	}
	if rows[1].Kind != RowMilestone || rows[1].GroupLabel != "v0.9" {
		t.Fatalf("row1 = %+v, want v0.9 milestone header", rows[1])
	}

	// Find the issue rows in the v0.9 group and check seq + parallel.
	seqByID := map[string]IssueRow{}
	for _, r := range rows {
		if r.Kind == RowIssue {
			seqByID[r.IssueID] = r
		}
	}
	if seqByID["390"].Seq != 1 {
		t.Errorf("390 seq = %d, want 1", seqByID["390"].Seq)
	}
	if seqByID["385"].Seq != 2 || !seqByID["385"].SeqParallel {
		t.Errorf("385 = seq %d parallel %v, want 2 / true", seqByID["385"].Seq, seqByID["385"].SeqParallel)
	}
	// The flat Infra issue has no in-group deps → Seq suppressed (0).
	if seqByID["100"].Seq != 0 {
		t.Errorf("100 seq = %d, want 0 (flat group)", seqByID["100"].Seq)
	}
}

func TestRollupOf_BreakdownAndCycle(t *testing.T) {
	members := []*linearapi.Issue{
		{StateType: "completed", Priority: 2},
		{StateType: "started", Priority: 1},                       // open urgent, in-progress
		{StateType: "backlog", Priority: 2, InCurrentCycle: true}, // open high, todo, in cycle
		{StateType: "backlog", Priority: 3},                       // open med, todo
		{StateType: "canceled", Priority: 1},                      // closed — excluded from breakdown
	}
	r := rollupOf(members)
	if r.count != 5 || r.done != 1 {
		t.Errorf("count/done = %d/%d, want 5/1", r.count, r.done)
	}
	if r.urgent != 1 || r.high != 1 || r.med != 1 || r.low != 0 {
		t.Errorf("priority breakdown = U%d H%d M%d L%d, want U1 H1 M1 L0", r.urgent, r.high, r.med, r.low)
	}
	if r.inProgress != 1 || r.todo != 2 {
		t.Errorf("state breakdown = ◐%d ○%d, want ◐1 ○2", r.inProgress, r.todo)
	}
	if !r.hasCurrentCycle {
		t.Error("hasCurrentCycle = false, want true")
	}
}

// Completed issues are counted in milestone progress even when their rows are hidden.
func TestBuildGroupedRows_HiddenDoneStillCounts(t *testing.T) {
	issues := []linearapi.Issue{
		{ID: "1", Identifier: "EFF-1", ProjectName: "P", MilestoneName: "v1", StateType: "completed"},
		{ID: "2", Identifier: "EFF-2", ProjectName: "P", MilestoneName: "v1", StateType: "completed"},
		{ID: "3", Identifier: "EFF-3", ProjectName: "P", MilestoneName: "v1", StateType: "backlog"},
	}
	hidden := map[string]bool{"completed": true}
	rows, _ := BuildGroupedRows(issues, map[string]bool{}, hidden)

	var ms IssueRow
	issueRows := 0
	for _, r := range rows {
		if r.Kind == RowMilestone {
			ms = r
		}
		if r.Kind == RowIssue {
			issueRows++
		}
	}
	// Progress reflects all 3 (2 done) even though only the 1 open row shows.
	if ms.GroupCount != 3 || ms.GroupDone != 2 {
		t.Errorf("milestone rollup = %d/%d, want 2/3", ms.GroupDone, ms.GroupCount)
	}
	if issueRows != 1 {
		t.Errorf("visible issue rows = %d, want 1 (done hidden)", issueRows)
	}
}

func TestBuildGroupedRows_CollapseHidesChildren(t *testing.T) {
	issues := []linearapi.Issue{
		{ID: "390", Identifier: "EFF-390", ProjectName: "Dory Q&A", MilestoneName: "v0.9", StateType: "completed"},
	}
	collapsed := map[string]bool{projectGroupKey("Dory Q&A"): true}
	rows, _ := BuildGroupedRows(issues, collapsed, map[string]bool{})
	for _, r := range rows {
		if r.Kind == RowIssue || r.Kind == RowMilestone {
			t.Errorf("collapsed project still emitted %+v", r)
		}
	}
	if len(rows) != 1 || rows[0].Kind != RowProject || !rows[0].Collapsed {
		t.Errorf("want a single collapsed project header, got %+v", rows)
	}
}
