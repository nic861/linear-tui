# Engineering Handoff Spec v1

## 1) Metadata
- Feature name: linear-tui blockedBy/blocks rendering
- Owner: Nic
- Date: 2026-04-24
- Repo: linear-tui
- Branch target: eff-277
- Related ticket(s): EFF-277
- Spec version: 1
- Tier: Full
- Baseline SHA: 3948ec3955eb1698916ba0c45fe9944aa3c2bb75
- Implementation SHA: pending
- Status: LOCKED
- Spec hash: f9b5487d96f396547d4ecbe3414898e03329cc0a81d3f1a776100b224814f563

## 2) Objective
- Problem statement: The `linear-tui` TUI renders Parent/Children hierarchy but ignores blocker relations. `blockedBy` is the canonical "what's next?" signal across all 15 epics in Linear, so users must context-switch to the Linear web app to see blocker state — defeating the point of a local TUI.
- Business/user outcome: The TUI answers the "what can I work on next?" question locally. Users see at a glance which issues are blocked and why, and which other issues each issue unblocks.
- Success metric(s):
  - Parser populates `Issue.BlockedBy` and `Issue.Blocks` for every fetched issue (verified by unit tests).
  - Details view shows "Blocked by:" and "Blocks:" sections on EFF-244 and EFF-276 (verified by unit tests on extracted pure function).
  - Tree rows with active blockers render in a distinct color (verified by unit test inspecting `tview.TableCell` color).

## 3) Scope
- In scope:
  - Add `IssueRelationRef` struct to `internal/linearapi/client.go`.
  - Add `BlockedBy []IssueRelationRef` and `Blocks []IssueRelationRef` fields to the `Issue` struct.
  - Extend all three inline GraphQL query structs (`searchIssues`, `issues`, `FetchIssueByID`) with matching `Relations` and `InverseRelations` selections including `state { name type }`.
  - Extend the reflection-based `parseIssueNode` to populate the new fields, with a shared helper `parseRelationRefs`.
  - Refactor `details_view.go` `updateDetailsView` to extract a pure `buildDetailsHeader(issue, themeTags, sectionGap) []string` helper; add "Blocked by:" and "Blocks:" sections mirroring the Sub-issues block.
  - Refactor `issues_table.go` row-rendering inline code at lines 316-332 to extract a pure `renderIdentifierCell(issue, theme, issueRow) *tview.TableCell` helper; apply `theme.StatusBlocked` color when the issue has open blockers.
  - Add `isBlocked(*Issue) bool` helper.
  - Add `StatusBlocked tcell.Color` field to the `Theme` struct and values to all three theme variants (`LinearTheme`, `HighContrastTheme`, `ColorBlindTheme`).
  - Add `TestParseIssueNode_Relations` + backfill `issueNodeJSON` with `state.type` and `cycle` (rolled in from audit OBS-1).
  - Add `TestIsBlocked` and `TestRenderIdentifierCell_BlockedColor` to `issues_table_test.go`.
  - Add new file `internal/tui/details_view_test.go` with tests for `buildDetailsHeader` covering Blocked by / Blocks / empty skip / Parent+Sub-issues regression.
  - Add assumption invalidation tests per Section 17.
- Out of scope:
  - Partial-unblock indicator (⚠) — issue marks it optional.
  - Blocker-graph visualization / "what's next" query mode — separate issue if needed.
  - Inline blocker editing (add/remove blockers via TUI) — separate issue.
  - Extracting a shared named `issueNode` type to collapse the triple inline-struct duplication (acknowledged by existing `//nolint:dupl` at `client.go:788`) — separate refactor, out of scope.
  - Contributing back upstream to `roeyazroel/linear-tui` — stay in the fork.
- Explicit non-goals:
  - Changing the existing Parent / Sub-issues rendering format, keyboard bindings, or tree expand/collapse glyphs.
  - Changing the Linear API endpoint used or the shurcooL/graphql library choice.
  - Adding a GraphQL `filter` argument on `relations` — deliberately client-side filtered (see Section 12).
- Major tradeoff: Added ~75 LOC of inline-struct duplication across three query sites (acknowledged by `//nolint:dupl`) rather than refactoring to a shared named type in this PR. Reason: shared-type extraction is a file-wide refactor (touches all query paths and parseIssueNode) that deserves its own review cycle — folding it in here would bloat the diff and risk collateral breakage on query paths unrelated to blocker rendering.
- Supersedes/depends: none — first pass at blocker rendering in the TUI.

## 4) Definitions
- **blockedBy**: In Linear's data model, the set of issues whose completion is required before this issue can proceed. Modeled via `IssueRelation` records where the current issue is the `relatedIssue` and the relation `type` is `"blocks"` — accessed via `Issue.inverseRelations(type: "blocks")`.
- **blocks**: The set of issues that cannot proceed until this issue is complete. Modeled via `IssueRelation` records where the current issue is the `issue` and the relation `type` is `"blocks"` — accessed via `Issue.relations(type: "blocks")`.
- **Open blocker**: An issue in a `BlockedBy` list whose `StateType` is neither `"completed"` nor `"canceled"`. Having at least one open blocker makes the issue "currently blocked."
- **Active blocker state types**: Any `StateType` value in `{backlog, unstarted, started}`. Anything not in `{completed, canceled}` is treated as active by `isBlocked`.
- **IssueRef / IssueChildRef / IssueRelationRef**: Three distinct Go structs representing issue references. IssueRef (used for Parent) has no state. IssueChildRef (used for Children) has state name but no state type. IssueRelationRef (NEW, used for BlockedBy/Blocks) has both state name and state type.

## 5) System Context
- Current behavior:
  - `internal/linearapi/client.go:161-212` — `IssueRef`, `IssueChildRef`, and `Issue` structs. `Issue` has `Parent *IssueRef` and `Children []IssueChildRef` but no blocker fields.
  - `internal/linearapi/client.go:651-711` — `searchIssuesPage` inline query struct with `Parent`/`Children` selections, no relations.
  - `internal/linearapi/client.go:814-874` — `fetchIssuesWithFilterPage` inline query struct with `Parent`/`Children` selections, no relations. Note `//nolint:dupl` at line 788.
  - `internal/linearapi/client.go:1053-1122` — `FetchIssueByID` inline query struct with `Parent`/`Children` selections, no relations.
  - `internal/linearapi/client.go:910-1045` — `parseIssueNode` reflection-based parser that populates Parent and Children from the query result. No relation handling.
  - `internal/tui/details_view.go:104-199` — `updateDetailsView` inline method on `*App` builds `headerLines` for the details panel. Parent rendered at 166-170. Sub-issues rendered at 172-187. No blocker rendering.
  - `internal/tui/issues_table.go:316-332` — inline row rendering builds `identifierPrefix+identifier` and calls `SetTextColor(theme.SecondaryText)` unconditionally. No blocker color logic.
  - `internal/tui/theme.go:11-28` — `Theme` struct has `StatusTodo`, `StatusInProgress`, `StatusDone`, `StatusCanceled` but no `StatusBlocked`.
  - `internal/tui/theme.go:40-89` — three theme variants (`LinearTheme`, `HighContrastTheme`, `ColorBlindTheme`) set all status colors.
- Desired behavior:
  - Every issue fetched via the three query code paths carries `Issue.BlockedBy` and `Issue.Blocks` populated with blocker relations (filtered client-side to `type=="blocks"`).
  - Details view shows a "Blocked by:" section (when non-empty) listing each blocker's identifier, state name, and title — mirroring Sub-issues format. Same for "Blocks:". Empty slices hide the section entirely.
  - Tree rows where the issue has at least one open blocker render the identifier cell in `theme.StatusBlocked` color. Other rows continue to use `theme.SecondaryText`.
  - Pure helpers (`isBlocked`, `parseRelationRefs`, `buildDetailsHeader`, `renderIdentifierCell`) are unit-tested without requiring a live tview terminal.
- Affected components/files/services:
  - `internal/linearapi/client.go`
  - `internal/linearapi/client_test.go`
  - `internal/tui/details_view.go`
  - `internal/tui/details_view_test.go` (NEW)
  - `internal/tui/issues_table.go`
  - `internal/tui/issues_table_test.go`
  - `internal/tui/theme.go`

## 6) Functional Requirements
| ID | Requirement | Priority | Rationale |
|---|---|---|---|
| FR-001 | Add `IssueRelationRef` struct with fields `ID, Identifier, Title, State, StateType string` in `internal/linearapi/client.go`. | P0 | Distinct from IssueRef/IssueChildRef because relation rendering requires state + state type; keeps Parent/Children contracts unchanged. |
| FR-002 | Add `BlockedBy []IssueRelationRef` and `Blocks []IssueRelationRef` fields to the `Issue` struct in `internal/linearapi/client.go`. | P0 | Exposes blocker data to presentation layer. |
| FR-003 | Extend all three inline GraphQL query structs (`searchIssues` ~line 705, `issues` ~line 868, `FetchIssueByID` ~line 1106) with matching `Relations` and `InverseRelations` sub-selections, each including `type` and `{relatedIssue\|issue} { id identifier title state { name type } }`. | P0 | shurcooL/graphql library builds queries from Go struct definitions; missing any one of the three causes the corresponding query path to return empty relations silently. |
| FR-004 | Add a `parseRelationRefs(nodesField reflect.Value, innerField string) []IssueRelationRef` helper in `internal/linearapi/client.go` that iterates the GraphQL nodes, filters `type != "blocks"`, and returns `IssueRelationRef` entries. | P0 | Shared extraction for Relations and InverseRelations parsing; avoids duplicating the ~15-LOC reflection loop. |
| FR-005 | Extend `parseIssueNode` in `internal/linearapi/client.go` (after the Children block) to call `parseRelationRefs` twice — once for `Relations` → `Blocks`, once for `InverseRelations` → `BlockedBy`. Populate the returned Issue struct fields. | P0 | Core data-flow wire-up from GraphQL response to Issue struct. |
| FR-006 | Refactor `updateDetailsView` in `internal/tui/details_view.go` to extract a pure `buildDetailsHeader(issue *linearapi.Issue, tags ThemeTags, sectionGap int) []string` function containing the header-line construction logic. `updateDetailsView` calls this helper and passes the result to `SetText` via `strings.Join`. Runtime behavior MUST be byte-identical to current for pre-existing sections (Parent, Sub-issues, State, Assignee, etc.). | P0 | Enables unit testing of the rendering logic without requiring a tview harness; satisfies AC coverage. |
| FR-007 | Add "Blocked by:" and "Blocks:" sections to `buildDetailsHeader`, rendered AFTER the Sub-issues block and BEFORE the divider. Each section header uses 12-char label alignment (`"Blocked by: "`, `"Blocks:     "`). Each entry formatted as `"  %s└─[-] %s%s[-] %s[%s][-] %s%s[-]"` (keyColor, accentColor+Identifier, keyColor+State, valColor+Title) — identical format to the existing Sub-issues loop. Each section is skipped when its slice has zero length. | P0 | User-visible outcome the feature exists for. |
| FR-008 | Add `isBlocked(issue *linearapi.Issue) bool` helper at file scope in `internal/tui/issues_table.go`. Returns `true` iff at least one entry in `issue.BlockedBy` has `StateType != "completed" && StateType != "canceled"`. | P0 | Single source of truth for blocked determination; used by the tree color logic and directly testable. |
| FR-009 | Refactor `internal/tui/issues_table.go` row render at lines 316-332 to extract a pure `renderIdentifierCell(issue *linearapi.Issue, theme Theme, issueRow IssueRow) *tview.TableCell` helper that returns a configured `*tview.TableCell` for the identifier column. Cell text is `identifierPrefix + identifier`. Cell color is `theme.StatusBlocked` when `isBlocked(issue)` is true, else `theme.SecondaryText`. Cell alignment is `tview.AlignLeft`. | P0 | Enables unit testing the blocked-color logic by inspecting the returned cell; satisfies AC coverage. |
| FR-010 | Add `StatusBlocked tcell.Color` field to the `Theme` struct in `internal/tui/theme.go` (after `StatusCanceled`). | P0 | Theme struct must declare the field before variants can set it. |
| FR-011 | Set `StatusBlocked` values in all three theme variants: `LinearTheme` = `tcell.NewRGBColor(200, 80, 80)` (#C85050), `HighContrastTheme` = `tcell.NewRGBColor(255, 60, 60)` (#FF3C3C), `ColorBlindTheme` = `tcell.NewRGBColor(204, 121, 167)` (#CC79A7 — Okabe-Ito reddish-purple, not red, to preserve the theme's color-blind-safe palette). | P0 | Visible color for the blocked indicator. Color-blind theme specifically avoids red to preserve accessibility. |

## 7) Behavioral Acceptance Criteria

### AC-001: Parser populates Blocks from relations[type=blocks]
Covers: FR-001, FR-002, FR-003, FR-004, FR-005
Given a GraphQL response JSON where the issue node has `relations.nodes` containing one entry with `type="blocks"` and `relatedIssue.identifier="BLOCK-A"`, `relatedIssue.state.type="started"`
When `parseIssueNode` processes this response (via any of the three query code paths)
Then the returned `Issue.Blocks` has length 1, with a value of Go type `IssueRelationRef` having `Identifier="BLOCK-A"` and `StateType="started"`, and `Issue.BlockedBy` is empty

### AC-002: Parser populates BlockedBy from inverseRelations[type=blocks]
Covers: FR-001, FR-002, FR-003, FR-004, FR-005
Given a GraphQL response JSON where the issue node has `inverseRelations.nodes` containing one entry with `type="blocks"` and `issue.identifier="BLOCKER-B"`, `issue.state.type="backlog"`
When `parseIssueNode` processes this response
Then the returned `Issue.BlockedBy` has length 1, with a value of Go type `IssueRelationRef` having `Identifier="BLOCKER-B"` and `StateType="backlog"`, and `Issue.Blocks` is empty

### AC-003: Parser filters out non-blocks relation types
Covers: FR-004, FR-005
Given a GraphQL response JSON with three `relations.nodes` entries: one `type="blocks"` identifier="B1", one `type="related"` identifier="R1", one `type="duplicate"` identifier="D1"
When `parseIssueNode` processes this response
Then `Issue.Blocks` has length exactly 1 containing "B1", and neither "R1" nor "D1" appear in Blocks or BlockedBy

### AC-004: All three query paths support relations
Covers: FR-001, FR-002, FR-003
Given the client has mock server responses that include `relations` and `inverseRelations` data
When `FetchIssueByID`, `FetchIssuesPage` (standard), and the search path all execute against the mock server
Then each returned `Issue` value exposes populated `Issue.BlockedBy` and `Issue.Blocks` slices (of type `[]IssueRelationRef`, non-nil and non-empty for the test fixture)

### AC-005: isBlocked returns true when any blocker is active
Covers: FR-008
Given an Issue with `BlockedBy = [{StateType: "started"}]`
When `isBlocked(&issue)` is called
Then it returns `true`

### AC-006: isBlocked returns false when BlockedBy is empty
Covers: FR-008
Given an Issue with nil or empty `BlockedBy`
When `isBlocked(&issue)` is called
Then it returns `false`

### AC-007: isBlocked returns false when all blockers are terminal
Covers: FR-008
Given an Issue with `BlockedBy = [{StateType: "completed"}, {StateType: "canceled"}]`
When `isBlocked(&issue)` is called
Then it returns `false`

### AC-008: Details header shows "Blocked by:" section when BlockedBy is non-empty
Covers: FR-006, FR-007
Given an Issue with `BlockedBy = [{Identifier: "EFF-239", State: "Todo", Title: "Centralize scope resolution", StateType: "unstarted"}]` and empty Blocks
When `buildDetailsHeader(issue, tags, sectionGap=1)` is called with valid ThemeTags
Then the returned `[]string` contains a line matching `/Blocked by:.*1 items/` and a line containing `"EFF-239"` and `"Centralize scope resolution"`, and contains no `/Blocks:/` line

### AC-009: Details header shows "Blocks:" section when Blocks is non-empty
Covers: FR-006, FR-007
Given an Issue with `Blocks = [{Identifier: "EFF-244", State: "In Progress", Title: "Feature Y", StateType: "started"}]` and empty BlockedBy
When `buildDetailsHeader(issue, tags, sectionGap=1)` is called
Then the returned `[]string` contains a line matching `/Blocks:.*1 items/` and a line containing `"EFF-244"` and `"Feature Y"`, and contains no `/Blocked by:/` line

### AC-010: Details header hides both sections when empty
Covers: FR-006, FR-007
Given an Issue with empty `BlockedBy` and empty `Blocks`
When `buildDetailsHeader(issue, tags, sectionGap=1)` is called
Then the returned `[]string` contains no line matching `/Blocked by:/` and no line matching `/Blocks:/`

### AC-011: Details header preserves existing sections (regression)
Covers: FR-006
Given an Issue with populated `Parent`, `Children`, `Labels`, `State`, `Assignee`, `Priority`, and `CycleName`
When `buildDetailsHeader(issue, tags, sectionGap=1)` is called
Then the returned `[]string` contains lines for each of: `State:`, `Assignee:`, `Priority:`, `Labels:`, `Parent:`, `Sub-issues:`, and at least one child row

### AC-012: Identifier cell uses StatusBlocked color when blocker is active
Covers: FR-008, FR-009, FR-010, FR-011
Given an Issue with `BlockedBy = [{StateType: "started"}]` and a `Theme` with `StatusBlocked = tcell.NewRGBColor(200, 80, 80)`
When `renderIdentifierCell(issue, theme, issueRow)` is called (with `issueRow.Level=0, HasChildren=false`)
Then the returned `*tview.TableCell` has `.Color == tcell.NewRGBColor(200, 80, 80)` (the StatusBlocked color), and the cell text equals `" " + issue.Identifier`

### AC-013: Identifier cell uses SecondaryText color when no blocker is active
Covers: FR-008, FR-009
Given an Issue with empty `BlockedBy` and a `Theme` with `SecondaryText = tcell.NewRGBColor(120, 120, 120)`
When `renderIdentifierCell(issue, theme, issueRow)` is called
Then the returned `*tview.TableCell` has `.Color == tcell.NewRGBColor(120, 120, 120)` (the SecondaryText color), NOT StatusBlocked

### AC-014: Identifier cell uses SecondaryText when all blockers are terminal
Covers: FR-008, FR-009
Given an Issue with `BlockedBy = [{StateType: "completed"}]` and a `Theme` with distinct `SecondaryText` and `StatusBlocked` colors
When `renderIdentifierCell(issue, theme, issueRow)` is called
Then the returned `*tview.TableCell` has `.Color == theme.SecondaryText`, NOT `theme.StatusBlocked`

### AC-015: All theme variants declare StatusBlocked
Covers: FR-010, FR-011
Given the three theme variants registered in `ThemeRegistry`
When each is inspected programmatically
Then `LinearTheme.StatusBlocked`, `HighContrastTheme.StatusBlocked`, and `ColorBlindTheme.StatusBlocked` are each a valid tcell.Color matching the RGB values specified in FR-011, and `ColorBlindTheme.StatusBlocked` is NOT in the set `{LinearTheme.StatusBlocked, HighContrastTheme.StatusBlocked}` (i.e., uses a distinct hue)

## 8) Non-Functional Requirements
| ID | Category | Target/Budget | Measurement Method | Fail Condition |
|---|---|---|---|---|
| NFR-001 | Build | `go build ./...` succeeds with no errors from the repo root of the worktree | Run `make build` or `go build ./...` | Any compile error |
| NFR-002 | Test pass rate | `go test ./...` exits 0 with all tests passing including the new ones | Run `make test` | Any test failure |
| NFR-003 | Lint | `make lint` (golangci-lint via `.golangci.yml`) exits 0 | Run `make lint` | Any new lint violation introduced by the change |
| NFR-004 | LOC budget | Implementation diff ≤ 220 LOC (excluding test files) | Count lines in `git diff origin/main..HEAD -- ':!*_test.go'` | Diff exceeds 220 LOC |
| NFR-005 | No regression on existing tests | All pre-existing tests (`TestFetchIssues_*`, `TestRenderIssueRow*`, `TestIssueRef`, `TestIssueChildRef`, etc.) continue to pass | Run `make test` | Any pre-existing test starts failing |

## 9) Data and Interface Contracts

```go
// NEW — added to internal/linearapi/client.go near IssueRef / IssueChildRef declarations.
type IssueRelationRef struct {
    ID         string
    Identifier string
    Title      string
    State      string // display name, e.g. "In Progress"
    StateType  string // lifecycle type, one of: backlog, unstarted, started, completed, canceled
}

// MODIFIED — additions to existing Issue struct (fields placed adjacent to Parent and Children).
type Issue struct {
    // ... existing fields unchanged ...
    Parent    *IssueRef
    Children  []IssueChildRef
    BlockedBy []IssueRelationRef // NEW — issues blocking this one (from inverseRelations type=blocks)
    Blocks    []IssueRelationRef // NEW — issues this one blocks (from relations type=blocks)
    // ... remaining existing fields unchanged ...
}

// NEW — shared parser helper in internal/linearapi/client.go.
// nodesField: reflect.Value for either Relations.Nodes or InverseRelations.Nodes.
// innerField: "RelatedIssue" for Relations, "Issue" for InverseRelations.
func parseRelationRefs(nodesField reflect.Value, innerField string) []IssueRelationRef

// NEW — pure helper in internal/tui/details_view.go.
// Returns the header text lines that used to be built inline in updateDetailsView.
// Same output (byte-identical) for all pre-existing sections.
func buildDetailsHeader(issue *linearapi.Issue, tags ThemeTags, sectionGap int) []string

// NEW — pure helper in internal/tui/issues_table.go.
// Returns a configured *tview.TableCell for the identifier column of one row.
// Color depends on isBlocked(issue).
type IssueRow struct {  // already exists in codebase — referenced here for clarity; do NOT redefine.
    IssueID     string
    Level       int
    HasChildren bool
    IsExpanded  bool
}
func renderIdentifierCell(issue *linearapi.Issue, theme Theme, row IssueRow) *tview.TableCell

// NEW — pure predicate in internal/tui/issues_table.go.
func isBlocked(issue *linearapi.Issue) bool

// MODIFIED — internal/tui/theme.go Theme struct gains:
type Theme struct {
    // ... existing fields unchanged ...
    StatusCanceled   tcell.Color
    StatusBlocked    tcell.Color // NEW
}
```

GraphQL query struct addition (applied identically to all three inline query structs in `client.go`):

```go
Relations struct {
    Nodes []struct {
        Type         graphql.String
        RelatedIssue struct {
            ID         graphql.String
            Identifier graphql.String
            Title      graphql.String
            State      struct {
                Name graphql.String
                Type graphql.String
            }
        }
    }
}
InverseRelations struct {
    Nodes []struct {
        Type  graphql.String
        Issue struct {
            ID         graphql.String
            Identifier graphql.String
            Title      graphql.String
            State      struct {
                Name graphql.String
                Type graphql.String
            }
        }
    }
}
```

## 10) Error Handling
| Case ID | Trigger | Expected Behavior | User-visible Message | Log Level |
|---|---|---|---|---|
| ERR-001 | GraphQL response contains `relations` or `inverseRelations` field with unexpected structure (e.g., missing `state` on inner issue) | Reflection parser treats missing string fields as empty string via `reflect.Value.String()` default; the `IssueRelationRef` entry has empty `State`/`StateType`. Empty state type causes `isBlocked` to return `true` (active) for that blocker, conservatively treating unknown blockers as active. | None — TUI renders the entry with empty state badge "[]" | n/a (graceful degradation) |
| ERR-002 | GraphQL response has zero relations or inverseRelations nodes | Parser produces empty `[]IssueRelationRef{}` (length 0, not nil — `make([]IssueRelationRef, 0, nodesField.Len())`). Details view skips the section. Tree does not color the row. | None | n/a |
| ERR-003 | `Relations` or `InverseRelations` field is missing entirely from the Go query struct (developer error — field not added to one of the three inline structs) | `v.FieldByName("Relations")` returns zero `reflect.Value`; `.FieldByName("Nodes")` on zero Value panics with `reflect: call of reflect.Value.FieldByName on zero Value`. This is a programmer bug caught by tests covering each query path (see AC-004, T-004 which calls FetchIssueByID and TestFetchIssues tests which call the `issues` path). | N/A — test-time failure, not runtime | n/a |
| ERR-004 | `IssueRelation.type` from Linear is a value other than `"blocks"` (e.g., `"related"`, `"duplicate"`, future types) | Parser silently skips the entry via the `rel.FieldByName("Type").String() != "blocks"` check. Only blocker relations populate the output. | None | n/a (expected filter) |
| ERR-005 | A blocker's `StateType` is an unrecognized value (e.g., a new Linear lifecycle type like `"archived"`) | `isBlocked` treats it as active (since it isn't `"completed"` or `"canceled"`). The issue shows as blocked. This is the conservative choice. | None | n/a |
| ERR-006 | Build fails because one of the three inline query structs has a typo in the Relations/InverseRelations field names (e.g., `RelatedIssue` vs `Related`) | Compile-time error surfaces before tests run. | N/A — compile failure | n/a |
| ERR-007 | Theme variant is missing `StatusBlocked` value (zero-valued tcell.Color) | `tview.TableCell.SetTextColor(zero-color)` may render unexpectedly. Test AC-015 asserts all three variants set this field, so this case is caught at test time. | N/A — test failure | n/a |

## 11) Security and Safety Requirements
- No secrets, tokens, or credentials are added or exposed by this change. The change reads additional fields from an already-authenticated Linear API session; no new authentication paths are introduced.
- No user-controlled input paths change. Blocker data comes from Linear's API; no new user input is parsed.
- Cache behavior: `internal/cache/team_cache.go` caches teams, users, projects, labels, and workflow states — it does NOT cache `Issue` structs. Adding fields to `Issue` has no cache-invalidation implications.
- No changes to HTTP transport, auth, or CORS configuration.
- No filesystem or shell operations added.

## 12) Implementation Constraints
- Allowed libraries: Only libraries already imported by `linear-tui` (shurcooL/graphql, gdamore/tcell/v2, rivo/tview, charmbracelet/glamour). Do NOT add new dependencies.
- Forbidden patterns:
  - Do NOT add a `filter: { type: { eq: "blocks" } }` argument to the `relations` or `inverseRelations` GraphQL field selection. Filtering is done client-side in `parseRelationRefs` (see Section 12 Patterns to follow). This is deliberate to avoid runtime GraphQL-schema errors if Linear's `IssueRelationConnection` does not accept the filter argument.
  - Do NOT add test harnesses that launch a real terminal (tview.Application.Run) — unit tests must run in `go test` without stdin/stdout special-casing.
  - Do NOT break the CONTRIBUTING.md conventions: exported PascalCase, unexported camelCase, one top-level type per file, `fmt.Errorf("context: %w", err)` for error wrapping.
  - Do NOT modify the existing test files' pre-existing test functions beyond the fixture backfill specified in FR-012.
  - Do NOT introduce a GraphQL `filter` on `relations`/`inverseRelations`. The Type filtering happens in the parser.
- Patterns to follow:
  - **GraphQL query struct duplication is expected:** the shurcooL/graphql library requires inline struct definitions. The `//nolint:dupl` at `client.go:788` acknowledges this. Copy the Relations/InverseRelations block to all three inline query structs identically.
  - **Reflection parser style:** match the existing `parseIssueNode` pattern — `v.FieldByName("X").FieldByName("Y").String()`. Use `make([]T, 0, fieldLen)` preallocation.
  - **Pure function extraction:** When refactoring `updateDetailsView` and the issues_table row-render loop, extract ONLY the logic needed for testability. Do NOT restructure unrelated code in the same functions. `buildDetailsHeader` produces a `[]string` that `updateDetailsView` joins with `"\n"` — the byte output of `SetText` must be unchanged for pre-existing sections.
  - **Client-side filter for relation type:** In `parseRelationRefs`, check `rel.FieldByName("Type").String() != "blocks"` and `continue` to skip. Exact string match (case-sensitive). Do not support "BLOCKS" or other variants.
  - **Color constants:** use `tcell.NewRGBColor(r, g, b)` form matching existing theme variants (see `theme.go:44-88` for examples).
  - **Section-gap in details header:** the pattern is `for i := 0; i < sectionGap; i++ { lines = append(lines, "") }` — reuse this between new sections and existing ones, as at `details_view.go:135, 174, 189, 193`.
  - **Label alignment in details_view:** section labels are padded to 12 visual chars (e.g., `"Parent:"` + 5 spaces = 12, `"Sub-issues:"` + 1 space = 12). New labels: `"Blocked by: "` (11 chars + 1 space = 12) and `"Blocks:     "` (7 chars + 5 spaces = 12).
  - **Supabase migrations:** N/A (no database in this repo).
- Implementation steps:
  1. Modify `internal/linearapi/client.go`: add `IssueRelationRef` struct near `IssueRef`/`IssueChildRef` (lines ~173). Add `BlockedBy` and `Blocks` fields to `Issue` struct.
  2. Modify `internal/linearapi/client.go`: add the `Relations` and `InverseRelations` sub-struct blocks to all three inline query structs at lines 651-711 (searchIssues), 814-874 (issues), and 1053-1122 (FetchIssueByID). The block is identical at each site.
  3. Modify `internal/linearapi/client.go`: add file-scope `parseRelationRefs` helper. Modify `parseIssueNode` to call it twice (for Blocks and BlockedBy) after the Children parsing block (~line 1008). Wire populated slices into the returned Issue.
  4. Modify `internal/linearapi/client_test.go`: backfill `issueNodeJSON` with `"state": {"id": "state-1", "name": "Todo", "type": "unstarted"}` and `"cycle": null` fields so existing tests continue to pass against the extended struct. Verify all pre-existing tests still pass before proceeding.
  5. Add `TestParseIssueNode_Relations` to `internal/linearapi/client_test.go` covering AC-001, AC-002, AC-003. Use a hand-crafted JSON fixture with mixed relation types.
  6. Modify `internal/tui/theme.go`: add `StatusBlocked tcell.Color` to the Theme struct (after `StatusCanceled`). Set values in `LinearTheme`, `HighContrastTheme`, `ColorBlindTheme` per FR-011.
  7. Modify `internal/tui/issues_table.go`: add file-scope `isBlocked` function. Extract `renderIdentifierCell` from the inline code at lines 316-332. Modify the caller to use the extracted helper.
  8. Add `TestIsBlocked` (AC-005, AC-006, AC-007, AC-014) and `TestRenderIdentifierCell_BlockedColor` (AC-012, AC-013) to `internal/tui/issues_table_test.go`.
  9. Modify `internal/tui/details_view.go`: extract `buildDetailsHeader` pure function containing the existing header-line construction. Ensure `updateDetailsView` still produces byte-identical output for pre-existing content. Add "Blocked by:" and "Blocks:" section rendering inside `buildDetailsHeader`.
  10. Create NEW file `internal/tui/details_view_test.go` with tests covering AC-008, AC-009, AC-010, AC-011.
  11. Run `go test ./...` and verify all tests pass. Run `make lint` and fix any new findings.
  12. Run Section 17 invalidation tests as part of the test suite.

## 13) Executable Test Plan (CLI-first)

All commands run from the worktree root: `/Users/nic/el/.worktrees/linear-tui/eff-277`.

| Test ID | Type | Covers | Command | Expected Result |
|---|---|---|---|---|
| T-001 | unit | AC-001, AC-002, AC-003 | `go test -run TestParseIssueNode_Relations ./internal/linearapi/...` | Exit 0; test passes. Asserts parser extracts Blocks, BlockedBy, and filters out non-blocks types. |
| T-002 | unit | NFR-005 | `go test -count=1 ./internal/linearapi/...` | Exit 0; all pre-existing linearapi tests continue to pass after the `issueNodeJSON` fixture backfill (see Section 12 Step 4). More surgical than T-012 — targets the package where the fixture change lives. |
| T-003 | unit | AC-005, AC-006, AC-007, AC-014 | `go test -run TestIsBlocked ./internal/tui/...` | Exit 0; isBlocked returns correct boolean for each of: empty BlockedBy, one active blocker, all terminal blockers, mixed (at least one active). |
| T-004 | unit | AC-004 | `go test -run TestQueryPaths_PopulateRelations ./internal/linearapi/...` | Exit 0; single consolidated test exercises the `FetchIssueByID`, `FetchIssuesPage`, and `searchIssuesPage` code paths against a mock HTTP server and asserts each returned Issue has populated BlockedBy and Blocks. |
| T-005 | unit | AC-012, AC-013, AC-014 | `go test -run TestRenderIdentifierCell_BlockedColor ./internal/tui/...` | Exit 0; cell color is StatusBlocked for open blocker, SecondaryText for no/terminal blockers. |
| T-006 | unit | AC-008 | `go test -run TestBuildDetailsHeader_BlockedBySection ./internal/tui/...` | Exit 0; header contains "Blocked by:" and blocker identifier/title when BlockedBy non-empty. |
| T-007 | unit | AC-009 | `go test -run TestBuildDetailsHeader_BlocksSection ./internal/tui/...` | Exit 0; header contains "Blocks:" and target identifier/title when Blocks non-empty. |
| T-008 | unit | AC-010 | `go test -run TestBuildDetailsHeader_EmptyRelationsSkipped ./internal/tui/...` | Exit 0; neither "Blocked by:" nor "Blocks:" appears when both slices are empty. |
| T-009 | unit | AC-011, FR-006 | `go test -run TestBuildDetailsHeader_PreservesExisting ./internal/tui/...` | Exit 0; Parent, Sub-issues, State, Assignee, Priority, Labels sections present with populated Issue. |
| T-010 | unit | AC-015, FR-010, FR-011 | `go test -run TestThemes_StatusBlockedSet ./internal/tui/...` | Exit 0; all three theme variants have valid, distinct StatusBlocked color; ColorBlindTheme uses reddish-purple (not red). |
| T-011 | build | NFR-001 | `go build ./...` | Exit 0; no compile errors. |
| T-012 | test-all | NFR-002, NFR-005 | `go test ./...` | Exit 0; ALL tests in the repo pass, including pre-existing and new. |
| T-013 | lint | NFR-003 | `make lint` | Exit 0; golangci-lint reports no new findings. |
| T-INV-001 | invalidation | ASM-001 | `go test -run TestParseIssueNode_Relations_CaseSensitiveFilter ./internal/linearapi/...` | Exit 0; with `type="BLOCKS"` (uppercase) in response, parsed Blocks and BlockedBy are both empty — documents case-sensitive filter. |
| T-INV-002 | invalidation | ASM-002 | `go test -run TestParseIssueNode_Relations_SemanticsNotSwapped ./internal/linearapi/...` | Exit 0; with distinct identifiers in relations vs inverseRelations, Issue.Blocks contains the relations identifier ONLY, and Issue.BlockedBy contains the inverseRelations identifier ONLY. |
| T-INV-003 | invalidation | ASM-003 | `go test -run TestIsBlocked_TerminalStatesFalse ./internal/tui/...` | Exit 0; isBlocked returns false when BlockedBy contains ONLY `{StateType: "completed"}` and `{StateType: "canceled"}` entries — proves the StateType gate is active. |
| T-INV-004 | invalidation | ASM-004 | `go test -run TestBuildDetailsHeader_NilRelationsNoHeader ./internal/tui/...` | Exit 0; with nil BlockedBy and nil Blocks, no section header for either appears in output. |
| T-INV-005 | invalidation | ASM-005 | `go test -run TestRenderIdentifierCell_TerminalBlockersNotColored ./internal/tui/...` | Exit 0; cell color is SecondaryText (NOT StatusBlocked) when all blockers are terminal. |

## 14) Definition of Done
- All FR-001 through FR-012 implemented as specified.
- All AC-001 through AC-015 covered by a passing T-* test.
- All ASM-001 through ASM-005 covered by a passing T-INV-* test.
- `go build ./...` exits 0 (T-011).
- `go test ./...` exits 0 with no failing tests, including all pre-existing tests (T-012).
- `make lint` exits 0 (T-013).
- Diff excluding test files ≤ 220 LOC (NFR-004).
- No unresolved blockers from `/audit --spec` or `/audit --code`.
- Manual sanity check (post-audit, recorded in review.md): the TUI runs locally with `LINEAR_API_KEY` set, and navigating to EFF-244 shows "Blocked by: EFF-239 [Todo] ..." in the details pane. (This check is not a gating automated test — the automated suite covers the render contract via `buildDetailsHeader` and `renderIdentifierCell`.)

## 15) Open Questions
### Q-001
- Question: Should we add a ThemeTags entry for StatusBlocked (to enable future text-markup usage of blocked color in details or help text)?
- Why it matters: Currently blocked color is only consumed via tview.TableCell.SetTextColor. If future work wants to color-tag blocker identifiers in the details view (e.g., blocked blockers shown in red text), a ThemeTags entry is needed.
- Owner: Nic
- Deadline: 2026-04-24
- Resolution: A-001

### A-001
- Decision: No — do not add to ThemeTags in this PR. Keep scope minimal; add when a caller needs it.
- Date: 2026-04-24
- Decider: Nic

### Q-002
- Question: Should `parseRelationRefs` be unexported (lowercase `parseRelationRefs`) or exported?
- Why it matters: If unexported, tests in the same package can access it; external callers cannot. If exported, the API surface expands.
- Owner: Nic
- Deadline: 2026-04-24
- Resolution: A-002

### A-002
- Decision: Unexported (`parseRelationRefs`, lowercase p). It's a helper for the package-internal `parseIssueNode` — no external caller needs it. Consistent with other internal helpers (`parseTime`).
- Date: 2026-04-24
- Decider: Nic

### Q-003
- Question: Should the blocked color ALSO be applied to the State column or Priority column, or only the Identifier column?
- Why it matters: UX question — does dimming the whole row convey "blocked" more clearly than just the identifier?
- Owner: Nic
- Deadline: 2026-04-24
- Resolution: A-003

### A-003
- Decision: Only the Identifier column. Matches the plan's design and keeps the visual signal minimal. The State column already has its own color semantics (StatusInProgress/Done/etc.) and shouldn't be overloaded. If user feedback says the single-column signal is too subtle, revisit in a follow-up.
- Date: 2026-04-24
- Decider: Nic

## 16) Adversarial Notes
- **Risk**: The shurcooL/graphql library might reject the Relations/InverseRelations Go struct shape at query serialization time if Linear's schema has a different field name or type signature. Prod deployment could fail with a GraphQL error on every fetch.
  **Mitigation**: T-004 explicitly exercises all three query code paths (FetchIssueByID, FetchIssuesPage, searchIssues) via a mock HTTP server that returns the expected shape. If the Go struct shape is wrong, tests fail. Additionally, manual verification against real Linear API before merging (see Section 14 manual sanity check).

- **Risk**: Linear's lifecycle state types may expand (e.g., a new `"archived"` or `"merged"` terminal state). Our `isBlocked` gate hardcodes `"completed"` and `"canceled"` — a new terminal state would silently count as "still blocking" and the issue would incorrectly render as blocked.
  **Mitigation**: ASM-003 / T-INV-003 documents the current allowlist. If real-world behavior shows this is wrong, the helper needs updating and the test serves as the change's documentation. This is the conservative choice — false positives (shows blocked when actually unblocked) are less dangerous than false negatives (shows unblocked when actually blocked).

- **Risk**: The `buildDetailsHeader` refactor could introduce a byte-for-byte difference in output vs the current inline implementation (e.g., a stray trailing space, a different Join separator). Users would see subtly different rendering.
  **Mitigation**: AC-011 explicitly asserts all pre-existing sections render in the refactored output. Before writing new "Blocked by:" / "Blocks:" rendering, the refactor step alone must keep all existing tests passing and produce byte-identical output on a sample Issue. Implementer should commit the refactor as its own commit, then add new sections in a follow-up commit.

- **Risk**: The `//nolint:dupl` comment suppresses a lint finding for the triple inline-struct duplication. Adding Relations/InverseRelations to all three sites preserves the suppression — but if a future linter or reviewer flags this, they may propose a refactor that changes the struct shape.
  **Mitigation**: OBS-2 is tracked as a separate refactor candidate (future Linear issue — not in this PR). Scope stays mechanical.

- **Risk**: `ColorBlindTheme` uses a reddish-purple (#CC79A7) to avoid red-only signaling. Some users may have manually customized their terminal palette such that #CC79A7 doesn't render as intended.
  **Mitigation**: Color is specified via tcell.NewRGBColor (24-bit true color) which bypasses palette remapping on supporting terminals. For legacy terminals, the nearest 256-color or 16-color approximation is used — acceptable fallback. User can edit the theme locally if needed. Documented in the theme.go comment for StatusBlocked.

## 17) Assumption Invalidation Tests

| ID | Assumption | Would break if... | Invalidation Test |
|----|-----------|-------------------|-------------------|
| ASM-001 | The parser's `type == "blocks"` check matches exactly how Linear emits the relation type string (lowercase, singular, no variation). | Linear changes the `IssueRelation.type` string to `"BLOCKS"`, `"Blocks"`, or a similar variant, silently making the client-side filter miss ALL blocker relations. Issue.BlockedBy and Issue.Blocks return empty slices despite Linear having the relations. User sees "no blockers" when blockers exist. | T-INV-001: `TestParseIssueNode_Relations_CaseSensitiveFilter` — feed response with `type="BLOCKS"` (uppercase), assert parsed Blocks and BlockedBy are both empty. Documents the case-sensitive filter behavior. If the behavior changes (filter becomes case-insensitive), this test fails and forces an explicit decision. |
| ASM-002 | `inverseRelations` populates `BlockedBy` (issues blocking THIS one) and `relations` populates `Blocks` (issues THIS one blocks). These semantics are asymmetric. | A refactor swaps the Relations/InverseRelations → Blocks/BlockedBy mapping (e.g., someone "fixes" what looks like a confusing name). Issue.BlockedBy shows issues I'm blocking instead of issues blocking me. User works on the wrong things. | T-INV-002: `TestParseIssueNode_Relations_SemanticsNotSwapped` — feed response where `relations.nodes[0].relatedIssue.identifier="BLOCKED-BY-ME"` and `inverseRelations.nodes[0].issue.identifier="BLOCKER-OF-ME"`. Assert `Issue.Blocks == ["BLOCKED-BY-ME"]` AND `Issue.BlockedBy == ["BLOCKER-OF-ME"]`. Distinct identifiers ensure the swap would cause test failure. |
| ASM-003 | `isBlocked` correctly uses `StateType` (lifecycle type) to differentiate active from terminal blockers. The allowlist of terminal types `{"completed", "canceled"}` is the exhaustive set in current Linear data. | A refactor removes the StateType check, making `isBlocked` return true whenever BlockedBy is non-empty. The tree colors rows red even when all blockers are done. Or: a blocker has empty StateType (edge case from malformed GraphQL response) and gets counted as active — this IS the conservative behavior we want. | T-INV-003: `TestIsBlocked_TerminalStatesFalse` — Issue with `BlockedBy = [{StateType:"completed"}, {StateType:"canceled"}]`. Assert `isBlocked(&issue) == false`. If the StateType gate is removed or negated, this test fails. |
| ASM-004 | `buildDetailsHeader` treats nil and empty-length slices identically — NEITHER produces a section header. | A refactor uses `issue.BlockedBy != nil` instead of `len(issue.BlockedBy) > 0`, which would emit the section header for explicitly-empty (non-nil, zero-length) slices. The details pane would show "Blocked by: 0 items" — visual noise, breaks the "hide when empty" AC. | T-INV-004: `TestBuildDetailsHeader_NilRelationsNoHeader` — Issue with `BlockedBy: nil, Blocks: nil`. Assert no line in the returned `[]string` matches `/Blocked by:/` or `/Blocks:/`. AND a second case: `BlockedBy: []IssueRelationRef{}` (empty slice). Same assertion. Both must pass. |
| ASM-005 | Only issues with at least one ACTIVE blocker (non-terminal StateType) get the StatusBlocked color. Issues with only terminal blockers look normal. | `renderIdentifierCell` uses `len(issue.BlockedBy) > 0` as the color gate instead of `isBlocked(issue)`. Rows with completed/canceled blockers render red even though the blocker is resolved. User thinks the issue is still blocked. | T-INV-005: `TestRenderIdentifierCell_TerminalBlockersNotColored` — Issue with `BlockedBy = [{StateType:"completed"}]`. Call `renderIdentifierCell(issue, theme, issueRow)`. Assert returned `cell.Color == theme.SecondaryText` (NOT `theme.StatusBlocked`). |

### Rules compliance notes
1. **Silent failures only.** ASM-001 through ASM-005 are all silent-failure scenarios — code runs, returns data, but the data or rendering is wrong. No crashes.
2. **Adversarial scenario first.** Each "Would break if..." column names a concrete realistic break mode before the test is designed.
3. **Test the break.** Each T-INV-* manufactures the exact condition that makes the assumption false. If the assumption holds (code is correct), the test passes. If a refactor breaks the assumption, the test fails.
4. **One assumption per test.** Each T-INV-NNN covers exactly one ASM-NNN.
5. **Five assumptions.** Meets the minimum 3 requirement.
6. **Auth:** N/A — no API routes touched; linear-tui is a client that consumes an already-authenticated API session.
