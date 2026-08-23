package tui

// Linear issue state types.
//
// These arrive from the API as bare strings on Issue.StateType and were compared
// as literals in ~15 places across four files, which is what goconst was pointing
// at. Naming them gives the vocabulary one home and makes a typo a compile error
// rather than a silently-never-true comparison.
//
// The set matches Linear's own state-type taxonomy; stateTypePickerOrder in app.go
// is the canonical ordering of it.
const (
	stateTypeStarted   = "started"
	stateTypeUnstarted = "unstarted"
	stateTypeTriage    = "triage"
	stateTypeBacklog   = "backlog"
	stateTypeCompleted = "completed"
	stateTypeCanceled  = "canceled"
	stateTypeDuplicate = "duplicate"
)
