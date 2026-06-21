package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/agents"
	"github.com/roeyazroel/linear-tui/internal/cache"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

// SortField represents a field to sort issues by.
type SortField string

const (
	SortByUpdatedAt      SortField = "updatedAt"
	SortByCreatedAt      SortField = "createdAt"
	SortByPriority       SortField = "priority"
	SortByProjectStatus  SortField = "projectStatus"  // project → status → priority
	SortByStatusPriority SortField = "statusPriority" // status → priority
	SortByCycle          SortField = "cycle"          // cycle → status → priority
	SortByMilestone      SortField = "milestone"      // project → milestone → status → priority
	SortByLabel          SortField = "label"          // group by label → status → priority
)

// App is the main application controller that manages all UI components.
type App struct {
	app       *tview.Application
	api       *linearapi.Client
	cache     *cache.TeamCache
	config    config.Config
	theme     Theme
	themeTags ThemeTags
	density   DensityProfile

	// UI components
	pages                  *tview.Pages
	mainLayout             *tview.Flex
	issuesTable            *tview.Table
	detailsView            *tview.Flex     // Flex container for details (description + comments)
	detailsDescriptionView *tview.TextView // Scrollable description/metadata view
	detailsCommentsView    *tview.TextView // Scrollable comments view
	statusBar              *tview.TextView
	paletteModal           *tview.Flex
	paletteInput           *tview.InputField
	paletteList            *tview.List
	paletteModalContent    *tview.Flex
	paletteCtrl            *PaletteController
	pickerModal            *PickerModal
	createIssueModal       *CreateIssueModal
	createCommentModal     *CreateCommentModal
	editTitleModal         *EditTitleModal
	editLabelsModal        *EditLabelsModal
	settingsModal          *SettingsModal
	promptTemplatesModal   *AgentPromptTemplatesModal
	agentPromptModal       *AgentPromptModal
	agentOutputModal       *AgentOutputModal
	agentRunner            *agents.Runner
	agentPromptTemplates   []config.AgentPromptTemplate

	// App state (protected by issuesMu)
	issuesMu      sync.RWMutex
	selectedIssue *linearapi.Issue
	issues        []linearapi.Issue
	focusedPane   FocusTarget

	// Issue tree state (for sub-issue hierarchy)
	issueRows       []IssueRow                  // Flattened rows for table rendering
	idToIssue       map[string]*linearapi.Issue // Quick lookup by issue ID
	expandedState   map[string]bool             // Expanded state for parent issues
	collapsedGroups map[string]bool             // Collapsed state for project/milestone headers (grouped view)
	foldLevel       int                         // Grouped-view fold level: 0=all collapsed, 1=milestone overview, 2=expanded

	// Filter/sort state
	searchQuery      string
	sortField        SortField
	hiddenStateTypes map[string]bool // state types to exclude (e.g., "completed", "canceled")

	// Cached metadata for currently selected team
	currentUser    *linearapi.User
	teamUsers      []linearapi.User
	workflowStates []linearapi.WorkflowState

	// Loading state
	isLoading                      bool
	pendingRefresh                 bool
	pendingRefreshIssueID          string
	pendingRefreshAllowFocusChange bool
	pickerActive                   bool
	refreshGeneration              atomic.Int64

	// Lazy loading helpers (overridable in tests)
	fetchIssuesPage func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error)
	fetchIssueByID  func(context.Context, string) (linearapi.Issue, error)
	queueUpdateDraw func(func())

	// UI update mutex (for test safety when queueUpdateDraw executes immediately)
	uiUpdateMu sync.Mutex

	// Race-safety for issue detail fetching
	fetchingIssueID string // Tracks which issue ID we're currently fetching

	// Details pane sub-view focus
	focusedDetailsView     bool // false = description, true = comments
	detailsCommentsVisible bool // Tracks whether comments view is shown

	// Pane resize state
	contentFlex   *tview.Flex // Horizontal split: issues | details
	splitRatio    float64     // 0.0–1.0, fraction of width for issues pane
	draggingSplit bool        // True while mouse is dragging the split border
}

// FocusTarget indicates which pane has focus.
type FocusTarget int

const (
	FocusIssues FocusTarget = iota
	FocusDetails
	FocusPalette
)

// NewApp creates a new application instance.
func NewApp(api *linearapi.Client, cfg config.Config, templates []config.AgentPromptTemplate) *App {
	if len(templates) == 0 {
		templates = config.DefaultAgentPromptTemplates()
	}
	theme := ResolveTheme(cfg.Theme)
	density := ResolveDensity(cfg.Density)

	app := &App{
		app:                  tview.NewApplication(),
		api:                  api,
		cache:                cache.NewTeamCache(api, cfg.CacheTTL),
		config:               cfg,
		theme:                theme,
		themeTags:            NewThemeTags(theme),
		density:              density,
		pages:                tview.NewPages(),
		focusedPane:          FocusIssues,
		sortField:            SortByProjectStatus,
		hiddenStateTypes:     map[string]bool{"completed": true, "canceled": true, "duplicate": true},
		expandedState:        make(map[string]bool),
		collapsedGroups:      make(map[string]bool),
		foldLevel:            2, // start fully expanded
		idToIssue:            make(map[string]*linearapi.Issue),
		agentPromptTemplates: templates,
		splitRatio:           0.5,
	}

	app.paletteCtrl = NewPaletteController(DefaultCommands(app))
	app.fetchIssuesPage = api.FetchIssuesPage
	app.fetchIssueByID = api.FetchIssueByID
	app.queueUpdateDraw = func(f func()) {
		app.app.QueueUpdateDraw(f)
	}

	app.applyThemeStyles()

	app.buildLayout()
	app.bindGlobalKeys()

	return app
}

// Run starts the application and blocks until it exits.
func (a *App) Run() error {
	a.app.SetRoot(a.pages, true).EnableMouse(true)

	// Enable drag-to-resize on the split border
	a.app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		x, _ := event.Position()

		switch action {
		case tview.MouseLeftDown:
			// Check if click is on (or within 1 cell of) the split border
			bx, _, bw, _ := a.contentFlex.GetRect()
			borderX := bx + int(a.splitRatio*float64(bw))
			if x >= borderX-1 && x <= borderX+1 {
				a.draggingSplit = true
				return nil, tview.MouseConsumed
			}
		case tview.MouseMove:
			if a.draggingSplit {
				bx, _, bw, _ := a.contentFlex.GetRect()
				if bw > 0 {
					ratio := float64(x-bx) / float64(bw)
					// Clamp to [0.15, 0.85] so neither pane disappears
					if ratio < 0.15 {
						ratio = 0.15
					} else if ratio > 0.85 {
						ratio = 0.85
					}
					a.splitRatio = ratio
					a.applySplitRatio()
				}
				return nil, tview.MouseConsumed
			}
		case tview.MouseLeftUp:
			if a.draggingSplit {
				a.draggingSplit = false
				return nil, tview.MouseConsumed
			}
		}

		return event, action
	})

	// Load initial data asynchronously
	a.loadInitialData()

	// Start the application event loop
	return a.app.Run()
}

// splitProportions converts the float splitRatio into integer proportions for tview.Flex.
func (a *App) splitProportions() (int, int) {
	left := int(a.splitRatio * 100)
	right := 100 - left
	if left < 1 {
		left = 1
	}
	if right < 1 {
		right = 1
	}
	return left, right
}

// applySplitRatio updates the contentFlex proportions to match the current splitRatio.
func (a *App) applySplitRatio() {
	leftProp, rightProp := a.splitProportions()
	a.contentFlex.ResizeItem(a.issuesTable, 0, leftProp)
	a.contentFlex.ResizeItem(a.detailsView, 0, rightProp)
}

// loadInitialData fetches user and issues in a background goroutine.
func (a *App) loadInitialData() {
	go func() {
		ctx := context.Background()

		// Fetch current user first
		user, err := a.cache.GetCurrentUser(ctx)
		if err == nil {
			a.currentUser = &user
			logger.Debug("tui.app: current user loaded user=%s", user.DisplayName)
		} else {
			logger.Warning("tui.app: failed to load current user error=%v", err)
		}

		// Load issues for initial view
		a.refreshIssues()
	}()
}

// applySettings updates runtime dependencies to match a new configuration.
func (a *App) applySettings(newCfg config.Config) {
	a.config = newCfg
	a.applyThemeAndDensity()

	logLevel := parseLogLevel(newCfg.LogLevel)
	if err := logger.Reinit(newCfg.LogFile, logLevel); err != nil {
		logger.ErrorWithErr(err, "tui.app: failed to reinitialize logger")
		a.QueueUpdateDraw(func() {
			a.updateStatusBarWithError(err)
		})
		return
	}
	logger.Debug("tui.app: settings applied log_file=%s log_level=%s", newCfg.LogFile, newCfg.LogLevel)

	a.api = linearapi.NewClient(linearapi.ClientConfig{
		Token:    newCfg.LinearAPIKey,
		Endpoint: newCfg.APIEndpoint,
		Timeout:  newCfg.Timeout,
	})
	a.cache = cache.NewTeamCache(a.api, newCfg.CacheTTL)
	a.fetchIssuesPage = a.api.FetchIssuesPage
	a.fetchIssueByID = a.api.FetchIssueByID

	logger.Debug("tui.app: resetting cached state after settings change")
	a.resetCachedState()
	a.loadInitialData()
}

func (a *App) applyThemeAndDensity() {
	a.theme = ResolveTheme(a.config.Theme)
	a.themeTags = NewThemeTags(a.theme)
	a.density = ResolveDensity(a.config.Density)

	a.applyThemeStyles()
	a.applyThemeToComponents()
	a.applyDensityToComponents()
	a.rebuildModals()
	a.updateStatusBar()
	a.updateDetailsView()
	a.updatePaletteList()
}

func (a *App) applyThemeStyles() {
	tview.Styles.PrimitiveBackgroundColor = a.theme.Background
	tview.Styles.ContrastBackgroundColor = a.theme.Background
	tview.Styles.MoreContrastBackgroundColor = a.theme.HeaderBg
	tview.Styles.BorderColor = a.theme.Border
	tview.Styles.TitleColor = a.theme.Foreground
	tview.Styles.GraphicsColor = a.theme.Border
	tview.Styles.PrimaryTextColor = a.theme.Foreground
	tview.Styles.SecondaryTextColor = a.theme.SecondaryText
	tview.Styles.TertiaryTextColor = a.theme.SecondaryText
	tview.Styles.InverseTextColor = a.theme.Background
	tview.Styles.ContrastSecondaryTextColor = a.theme.SecondaryText
}

func (a *App) applyThemeToComponents() {
	if a.issuesTable != nil {
		a.applyIssuesTableTheme(a.issuesTable)
		renderIssuesTableModel(a.issuesTable, a.issueRows, a.idToIssue, a.currentSelectedIssueID(), a.theme, a.grouped())
	}

	if a.detailsDescriptionView != nil {
		a.detailsDescriptionView.SetTitleColor(a.theme.Foreground).
			SetBorderColor(a.theme.Border).
			SetBackgroundColor(a.theme.Background)
	}
	if a.detailsCommentsView != nil {
		a.detailsCommentsView.SetTitleColor(a.theme.Foreground).
			SetBorderColor(a.theme.Border).
			SetBackgroundColor(a.theme.Background)
	}

	if a.statusBar != nil {
		a.statusBar.SetBackgroundColor(a.theme.HeaderBg)
	}
}

func (a *App) applyDensityToComponents() {
	if a.detailsDescriptionView != nil {
		padding := a.density.DetailsPadding
		a.detailsDescriptionView.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)
	}
	if a.detailsCommentsView != nil {
		padding := a.density.DetailsPadding
		a.detailsCommentsView.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)
	}
	if a.statusBar != nil {
		padding := a.density.StatusBarPadding
		a.statusBar.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)
	}
	if a.agentOutputModal != nil {
		a.agentOutputModal.ApplyDensity(a.density)
	}
}

func (a *App) rebuildModals() {
	if a.pages != nil {
		a.pages.RemovePage("palette")
	}
	a.paletteModal = a.buildPaletteModal()
	if a.pages != nil {
		a.pages.AddPage("palette", a.paletteModal, true, false)
	}

	a.pickerModal = NewPickerModal(a)
	a.createIssueModal = NewCreateIssueModal(a)
	a.createCommentModal = NewCreateCommentModal(a)
	a.editTitleModal = NewEditTitleModal(a)
	a.editLabelsModal = NewEditLabelsModal(a)
	a.settingsModal = NewSettingsModal(a)
	a.promptTemplatesModal = NewAgentPromptTemplatesModal(a)
	a.agentPromptModal = NewAgentPromptModal(a)
	if a.pages == nil || !a.pages.HasPage("agent_output") {
		a.agentOutputModal = NewAgentOutputModal(a)
	} else {
		a.agentOutputModal.ApplyTheme(a.theme)
		a.agentOutputModal.ApplyDensity(a.density)
	}
}

func (a *App) applyIssuesTableTheme(table *tview.Table) {
	if table == nil {
		return
	}
	table.SetTitleColor(a.theme.Foreground).
		SetBorderColor(a.theme.Border).
		SetBackgroundColor(a.theme.Background)
	table.SetSelectedStyle(tcell.StyleDefault.
		Foreground(a.theme.SelectionText).
		Background(a.theme.SelectionBg).
		Bold(true))
}

func (a *App) currentSelectedIssueID() string {
	if a.issuesTable == nil {
		return ""
	}
	row, _ := a.issuesTable.GetSelection()
	if row <= 0 {
		return ""
	}
	issue := a.getIssueFromRow(row)
	if issue == nil {
		return ""
	}
	return issue.ID
}

// resetCachedState clears cached user and issue data after config changes.
func (a *App) resetCachedState() {
	a.issuesMu.Lock()
	a.selectedIssue = nil
	a.issues = nil
	a.issueRows = nil
	a.idToIssue = make(map[string]*linearapi.Issue)
	a.issuesMu.Unlock()

	a.currentUser = nil
	a.teamUsers = nil
	a.workflowStates = nil
	a.expandedState = make(map[string]bool)
	a.collapsedGroups = make(map[string]bool)
	a.hiddenStateTypes = map[string]bool{"completed": true, "canceled": true, "duplicate": true}

	a.isLoading = false
	a.pendingRefresh = false
	a.pendingRefreshIssueID = ""
	a.pendingRefreshAllowFocusChange = true
	// Bump generation to prevent in-flight refreshes from updating UI.
	a.refreshGeneration.Add(1)
	a.fetchingIssueID = ""
}

// parseLogLevel converts a string log level to a logger.LogLevel.
func parseLogLevel(level string) logger.LogLevel {
	switch level {
	case "debug":
		return logger.LevelDebug
	case "info":
		return logger.LevelInfo
	case "warning":
		return logger.LevelWarning
	case "error":
		return logger.LevelError
	default:
		return logger.LevelWarning
	}
}

// buildLayout constructs the main UI layout.
func (a *App) buildLayout() {
	// Build all panes
	a.issuesTable = a.buildIssuesTable(" Issues ")
	a.detailsView = a.buildDetailsView()
	a.statusBar = a.buildStatusBar()

	// Create horizontal split with proportions derived from splitRatio
	leftProp, rightProp := a.splitProportions()
	a.contentFlex = tview.NewFlex().
		AddItem(a.issuesTable, 0, leftProp, true).
		AddItem(a.detailsView, 0, rightProp, false)

	// Create vertical layout: content + status bar
	a.mainLayout = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(a.contentFlex, 0, 1, true).
		AddItem(a.statusBar, 1, 1, false)

	// Build palette modal
	a.paletteModal = a.buildPaletteModal()

	// Build picker and create issue modals
	a.pickerModal = NewPickerModal(a)
	a.createIssueModal = NewCreateIssueModal(a)
	a.createCommentModal = NewCreateCommentModal(a)
	a.editTitleModal = NewEditTitleModal(a)
	a.editLabelsModal = NewEditLabelsModal(a)
	a.settingsModal = NewSettingsModal(a)
	a.promptTemplatesModal = NewAgentPromptTemplatesModal(a)
	a.agentPromptModal = NewAgentPromptModal(a)
	a.agentOutputModal = NewAgentOutputModal(a)
	a.agentRunner = agents.NewRunner()

	// Add main layout to pages
	a.pages.AddPage("main", a.mainLayout, true, true)
	a.pages.AddPage("palette", a.paletteModal, true, false)

	// Set initial focus
	a.updateFocus()
}

// bindGlobalKeys sets up global keyboard shortcuts.
func (a *App) bindGlobalKeys() {
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Handle picker modal if active
		if a.pickerActive {
			return a.pickerModal.HandleKey(event)
		}

		// Check if create issue modal is visible and handle its keys
		if a.pages.HasPage("create_issue") && a.createIssueModal != nil {
			return a.createIssueModal.HandleKey(event)
		}

		// Check if create comment modal is visible and handle its keys
		if a.pages.HasPage("create_comment") && a.createCommentModal != nil {
			return a.createCommentModal.HandleKey(event)
		}

		// Check if edit title modal is visible and handle its keys
		if a.pages.HasPage("edit_title") && a.editTitleModal != nil {
			return a.editTitleModal.HandleKey(event)
		}

		// Check if edit labels modal is visible and handle its keys
		if a.pages.HasPage("edit_labels") && a.editLabelsModal != nil {
			return a.editLabelsModal.HandleKey(event)
		}

		// Check if settings modal is visible and handle its keys
		if a.pages.HasPage("settings") && a.settingsModal != nil {
			return a.settingsModal.HandleKey(event)
		}

		// Check if prompt templates modal is visible and handle its keys
		if a.pages.HasPage("prompt_templates") && a.promptTemplatesModal != nil {
			return a.promptTemplatesModal.HandleKey(event)
		}

		// Check if agent prompt modal is visible and handle its keys
		if a.pages.HasPage("agent_prompt") && a.agentPromptModal != nil {
			return a.agentPromptModal.HandleKey(event)
		}

		// Check if agent output modal is visible and handle its keys
		if a.pages.HasPage("agent_output") && a.agentOutputModal != nil {
			return a.agentOutputModal.HandleKey(event)
		}

		// Handle palette first if it's open
		if a.focusedPane == FocusPalette {
			return a.handlePaletteKey(event)
		}

		// Global shortcuts (only when not in palette)
		switch event.Key() {
		case tcell.KeyEscape:
			// Clear search if active (when not in modals/palette)
			if a.searchQuery != "" {
				a.setSearchQuery("")
				return nil
			}
		case tcell.KeyCtrlC:
			a.app.Stop()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			// Tab cycles forward through panes (Navigation -> Issues -> Details)
			// When in Details pane, first cycle between description and comments
			// Only cycle when not in palette or modals
			isBackward := event.Key() == tcell.KeyBacktab || event.Modifiers()&tcell.ModShift != 0
			if a.focusedPane != FocusPalette {
				if a.focusedPane == FocusDetails {
					if !a.detailsCommentsVisible {
						if isBackward {
							a.cyclePanesBackward()
						} else {
							a.cyclePanesForward()
						}
						return nil
					}
					// Cycle between description and comments within details pane
					if !isBackward {
						// Tab: description -> comments -> next pane
						if a.focusedDetailsView {
							// Currently on comments, move to next pane
							a.focusedDetailsView = false // Reset for next time
							a.cyclePanesForward()
						} else {
							// Currently on description, move to comments
							a.focusedDetailsView = true
							a.updateFocus()
						}
					} else {
						// Shift+Tab: comments -> description -> previous pane
						if a.focusedDetailsView {
							// Currently on comments, move to description
							a.focusedDetailsView = false
							a.updateFocus()
						} else {
							// Currently on description, move to previous pane
							a.cyclePanesBackward()
						}
					}
				} else {
					if isBackward {
						// Shift+Tab cycles backward
						a.cyclePanesBackward()
					} else {
						a.cyclePanesForward()
					}
				}
			}
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'q':
				a.app.Stop()
				return nil
			case ':':
				a.openPalette()
				return nil
			case '/':
				a.openSearchPalette()
				return nil
			}
		}

		// Pane-specific shortcuts
		switch a.focusedPane {
		case FocusIssues:
			return a.handleIssuesKey(event)
		case FocusDetails:
			return a.handleDetailsKey(event)
		}

		return event
	})
}

// handleIssuesKey handles keyboard input when issues pane is focused.
func (a *App) handleIssuesKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyRight:
		a.focusedPane = FocusDetails
		a.focusedDetailsView = false // Start with description
		a.updateFocus()
		return nil
	case tcell.KeyRune:
		r := event.Rune()
		// Handle vim-style navigation first
		if r == 'l' {
			a.focusedPane = FocusDetails
			a.focusedDetailsView = false // Start with description
			a.updateFocus()
			return nil
		}
		// Handle command shortcuts (plain letters) - skip navigation keys
		if r != 'j' && r != 'k' { // j/k are handled by table for up/down
			for _, cmd := range a.paletteCtrl.commands {
				if cmd.ShortcutRune != 0 && cmd.ShortcutRune == r {
					cmd.Run(a)
					return nil
				}
			}
		}
	}
	return event
}

// handleDetailsKey handles keyboard input when details pane is focused.
func (a *App) handleDetailsKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyLeft:
		a.focusedPane = FocusIssues
		a.updateFocus()
		return nil
	case tcell.KeyRune:
		if event.Rune() == 'h' {
			a.focusedPane = FocusIssues
			a.updateFocus()
			return nil
		}
	}
	return event
}

// handlePaletteKey handles keyboard input when palette is open.
func (a *App) handlePaletteKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		if a.paletteCtrl.IsSearchMode() {
			// In search mode, clear search and close palette
			a.closePaletteUI()
			a.setSearchQuery("")
			return nil
		}
		a.closePalette()
		return nil
	case tcell.KeyEnter:
		if a.paletteCtrl.IsSearchMode() {
			// In search mode, submit the search query
			query := a.paletteCtrl.Query()
			a.closePaletteUI()      // Close UI without changing focus
			a.setSearchQuery(query) // This will set focus to issues pane
			return nil
		}
		// In command mode, execute the selected command
		if cmd, ok := a.paletteCtrl.Selected(); ok {
			a.closePalette()
			cmd.Run(a)
			return nil
		}
		return nil
	case tcell.KeyUp:
		if !a.paletteCtrl.IsSearchMode() {
			a.paletteCtrl.MoveCursorUp()
			a.updatePaletteList()
		}
		return nil
	case tcell.KeyDown:
		if !a.paletteCtrl.IsSearchMode() {
			a.paletteCtrl.MoveCursorDown()
			a.updatePaletteList()
		}
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		query := a.paletteCtrl.Query()
		if len(query) > 0 {
			a.paletteCtrl.SetQuery(query[:len(query)-1])
			a.paletteInput.SetText(a.paletteCtrl.Query())
			if !a.paletteCtrl.IsSearchMode() {
				a.updatePaletteList()
			}
		}
		return nil
	case tcell.KeyRune:
		query := a.paletteCtrl.Query() + string(event.Rune())
		a.paletteCtrl.SetQuery(query)
		a.paletteInput.SetText(query)
		if !a.paletteCtrl.IsSearchMode() {
			a.updatePaletteList()
		}
		return nil
	}
	return event
}

// cyclePanesForward cycles focus forward: Issues -> Details -> Issues
func (a *App) cyclePanesForward() {
	switch a.focusedPane {
	case FocusIssues:
		a.focusedPane = FocusDetails
		a.focusedDetailsView = false
	case FocusDetails:
		a.focusedPane = FocusIssues
	}
	a.updateFocus()
}

// cyclePanesBackward cycles focus backward: Details -> Issues -> Details
func (a *App) cyclePanesBackward() {
	switch a.focusedPane {
	case FocusIssues:
		a.focusedPane = FocusDetails
		a.focusedDetailsView = false
	case FocusDetails:
		a.focusedPane = FocusIssues
	}
	a.updateFocus()
}

// updateFocus updates the focus state of all panes.
func (a *App) updateFocus() {
	switch a.focusedPane {
	case FocusIssues:
		a.app.SetFocus(a.issuesTable)
		a.issuesTable.SetBorderColor(a.theme.BorderFocus)
		a.detailsDescriptionView.SetBorderColor(a.theme.Border)
		a.detailsCommentsView.SetBorderColor(a.theme.Border)
		a.updateAllPaneTitles()
	case FocusDetails:
		if !a.detailsCommentsVisible {
			a.focusedDetailsView = false
		}
		if a.focusedDetailsView && a.detailsCommentsVisible {
			a.app.SetFocus(a.detailsCommentsView)
			a.detailsDescriptionView.SetBorderColor(a.theme.Border)
			a.detailsCommentsView.SetBorderColor(a.theme.BorderFocus)
		} else {
			a.app.SetFocus(a.detailsDescriptionView)
			a.detailsDescriptionView.SetBorderColor(a.theme.BorderFocus)
			a.detailsCommentsView.SetBorderColor(a.theme.Border)
		}
		a.issuesTable.SetBorderColor(a.theme.Border)
		a.updateAllPaneTitles()
	case FocusPalette:
		a.app.SetFocus(a.paletteInput)
		a.issuesTable.SetBorderColor(a.theme.Border)
		a.detailsDescriptionView.SetBorderColor(a.theme.Border)
		a.detailsCommentsView.SetBorderColor(a.theme.Border)
		a.updateAllPaneTitles()
	}
	a.updateStatusBar()
}

// updateAllPaneTitles updates all pane titles with visual indicators for the active pane.
func (a *App) updateAllPaneTitles() {
	// Update Issues pane title
	if a.focusedPane == FocusIssues {
		a.issuesTable.SetTitle(" ▶ Issues ")
		a.issuesTable.SetTitleColor(a.theme.Accent)
	} else {
		a.issuesTable.SetTitle(" Issues ")
		a.issuesTable.SetTitleColor(a.theme.Foreground)
	}

	// Update Details pane titles
	isDetailsFocused := a.focusedPane == FocusDetails
	if a.detailsDescriptionView != nil {
		if isDetailsFocused {
			// Details pane is focused - show indicator on active sub-view
			if a.focusedDetailsView && a.detailsCommentsVisible && a.detailsCommentsView != nil {
				// Comments view is active
				a.detailsDescriptionView.SetTitle(" Details ")
				a.detailsDescriptionView.SetTitleColor(a.theme.Foreground)
				a.detailsCommentsView.SetTitle(" ▶ Comments ")
				a.detailsCommentsView.SetTitleColor(a.theme.Accent)
			} else {
				// Description view is active
				a.detailsDescriptionView.SetTitle(" ▶ Details ")
				a.detailsDescriptionView.SetTitleColor(a.theme.Accent)
				if a.detailsCommentsVisible && a.detailsCommentsView != nil {
					a.detailsCommentsView.SetTitle(" Comments ")
					a.detailsCommentsView.SetTitleColor(a.theme.Foreground)
				}
			}
		} else {
			// Details pane is not focused - reset both titles
			a.detailsDescriptionView.SetTitle(" Details ")
			a.detailsDescriptionView.SetTitleColor(a.theme.Foreground)
			if a.detailsCommentsView != nil {
				a.detailsCommentsView.SetTitle(" Comments ")
				a.detailsCommentsView.SetTitleColor(a.theme.Foreground)
			}
		}
	}
}

// openPalette opens the command palette overlay.
func (a *App) openPalette() {
	a.paletteCtrl.Reset()
	a.paletteInput.SetText("")
	a.paletteInput.SetLabel("> ")
	a.updatePaletteList()
	a.pages.ShowPage("palette")
	a.pages.SendToFront("palette")
	a.focusedPane = FocusPalette
	a.updateFocus()
}

// openSearchPalette opens the palette in search mode.
func (a *App) openSearchPalette() {
	a.paletteCtrl.SetSearchMode(true)
	a.paletteCtrl.SetQuery(a.searchQuery)
	a.paletteInput.SetText(a.searchQuery)
	a.paletteInput.SetLabel("/ ")
	a.paletteList.Clear()
	a.pages.ShowPage("palette")
	a.pages.SendToFront("palette")
	a.focusedPane = FocusPalette
	a.updateFocus()
}

// closePalette closes the command palette overlay.
func (a *App) closePalette() {
	a.paletteCtrl.SetSearchMode(false)
	a.pages.HidePage("palette")
	a.focusedPane = FocusIssues
	a.updateFocus()
}

// closePaletteUI closes the palette UI without changing focus.
// This is used when focus will be set by the caller (e.g., after search).
func (a *App) closePaletteUI() {
	a.paletteCtrl.SetSearchMode(false)
	a.pages.HidePage("palette")
}

// queueIssuesRefresh records a refresh request while a fetch is in progress.
func (a *App) queueIssuesRefresh(allowFocusChange bool, issueID ...string) {
	logger.Debug("tui.app: queueing issues refresh issue_id=%v", issueID)
	a.pendingRefresh = true
	a.pendingRefreshAllowFocusChange = allowFocusChange
	a.refreshGeneration.Add(1)
	if len(issueID) > 0 {
		a.pendingRefreshIssueID = issueID[0]
		return
	}
	a.pendingRefreshIssueID = ""
}

// runQueuedIssuesRefresh triggers any queued refresh after a fetch completes.
func (a *App) runQueuedIssuesRefresh() {
	if !a.pendingRefresh {
		return
	}
	issueID := a.pendingRefreshIssueID
	allowFocusChange := a.pendingRefreshAllowFocusChange
	logger.Debug("tui.app: running queued refresh issue_id=%s", issueID)
	a.pendingRefresh = false
	a.pendingRefreshIssueID = ""
	a.pendingRefreshAllowFocusChange = true
	if issueID != "" {
		go a.refreshIssuesWithFocusChange(allowFocusChange, issueID)
		return
	}
	go a.refreshIssuesWithFocusChange(allowFocusChange)
}

// refreshIssues fetches issues from the API and updates the UI.
// If issueID is provided, that issue will be selected after refresh.
func (a *App) refreshIssues(issueID ...string) {
	a.refreshIssuesWithFocusChange(true, issueID...)
}

// refreshIssuesWithFocusChange fetches issues and optionally shifts focus to the issues pane.
func (a *App) refreshIssuesWithFocusChange(allowFocusChange bool, issueID ...string) {
	if a.isLoading {
		a.queueIssuesRefresh(allowFocusChange, issueID...)
		return
	}
	a.isLoading = true

	targetID := ""
	if len(issueID) > 0 {
		targetID = issueID[0]
	}
	logger.Debug("tui.app: starting issues refresh target_issue_id=%s", targetID)
	generation := a.refreshGeneration.Add(1)
	var targetIssueID string
	if len(issueID) > 0 {
		targetIssueID = issueID[0]
	}

	allowFocus := allowFocusChange
	go func() {
		ctx := context.Background()

		// Custom sort modes use client-side sorting, fetch with updatedAt from API.
		apiOrderBy := string(a.sortField)
		switch a.sortField {
		case SortByProjectStatus, SortByStatusPriority, SortByCycle, SortByMilestone, SortByLabel:
			apiOrderBy = string(SortByUpdatedAt)
		}

		// Build excluded state types list from hiddenStateTypes map.
		// In the grouped (milestone) view we still COUNT completed issues toward
		// milestone progress, so we must fetch them even when they're hidden — their
		// rows are filtered out at build time, but the rollup needs them.
		var excludeTypes []string
		for st := range a.hiddenStateTypes {
			if a.grouped() && st == "completed" {
				continue
			}
			excludeTypes = append(excludeTypes, st)
		}

		params := linearapi.FetchIssuesParams{
			First:             a.config.PageSize,
			Search:            a.searchQuery,
			OrderBy:           apiOrderBy,
			ExcludeStateTypes: excludeTypes,
		}

		fetchPage := a.fetchIssuesPage
		if fetchPage == nil {
			fetchPage = a.api.FetchIssuesPage
		}

		pageCount := 0
		fetchedCount := 0
		logger.Debug("tui.app: refreshing issues team_id=%s project_id=%s state_id=%s search=%s", params.TeamID, params.ProjectID, params.StateID, params.Search)
		page, err := fetchPage(ctx, params, nil)
		if err != nil {
			a.QueueUpdateDraw(func() {
				a.isLoading = false
				logger.ErrorWithErr(err, "tui.app: failed to fetch issues")
				a.updateStatusBarWithError(err)
				a.runQueuedIssuesRefresh()
			})
			return
		}
		if generation != a.refreshGeneration.Load() {
			a.QueueUpdateDraw(func() {
				a.isLoading = false
				a.runQueuedIssuesRefresh()
			})
			return
		}

		pageCount++
		fetchedCount += len(page.Issues)
		a.QueueUpdateDraw(func() {
			logger.Debug("tui.app: fetched issues page=%d count=%d", pageCount, len(page.Issues))
			a.updateIssuesData(page.Issues, targetIssueID)
			if allowFocus {
				// Ensure focus is on issues table after initial load
				a.focusedPane = FocusIssues
				a.updateFocus()
			}
			if page.HasNext {
				a.statusBar.SetText(fmt.Sprintf("%sLoading more (page %d, fetched %d)...[-]", a.themeTags.Warning, pageCount, fetchedCount))
			}
		})

		after := page.EndCursor
		for page.HasNext {
			if generation != a.refreshGeneration.Load() {
				break
			}
			nextPage, err := fetchPage(ctx, params, after)
			if err != nil {
				a.QueueUpdateDraw(func() {
					logger.ErrorWithErr(err, "tui.app: failed to fetch more issues page=%d", pageCount+1)
					a.updateStatusBarWithError(err)
				})
				break
			}
			if generation != a.refreshGeneration.Load() {
				break
			}

			page = nextPage
			after = page.EndCursor
			pageCount++
			fetchedCount += len(page.Issues)
			a.QueueUpdateDraw(func() {
				a.appendIssuesData(page.Issues)
				if page.HasNext {
					a.statusBar.SetText(fmt.Sprintf("%sLoading more (page %d, fetched %d)...[-]", a.themeTags.Warning, pageCount, fetchedCount))
				}
			})
		}

		a.QueueUpdateDraw(func() {
			a.isLoading = false
			logger.Debug("tui.app: refresh completed pages=%d total_fetched=%d", pageCount, fetchedCount)
			a.updateStatusBar()
			a.runQueuedIssuesRefresh()
		})
	}()

	// Show loading indicator
	a.QueueUpdateDraw(func() {
		a.statusBar.SetText(fmt.Sprintf("%sLoading...[-]", a.themeTags.Warning))
	})
}

// updateIssuesData updates the UI with new issues data.
// If issueID is provided, that issue will be selected if found in the list.
func (a *App) updateIssuesData(issues []linearapi.Issue, issueID ...string) {
	a.issuesMu.Lock()
	a.issues = issues
	sortIssues(a.issues, a.sortField)

	// Determine target issue ID
	var targetIssueID string
	if len(issueID) > 0 && issueID[0] != "" {
		targetIssueID = issueID[0]
	} else if a.selectedIssue != nil {
		targetIssueID = a.selectedIssue.ID
	}
	a.issuesMu.Unlock()

	selectedIssue := a.rebuildIssuesTable(targetIssueID)
	if selectedIssue != nil {
		a.onIssueSelected(*selectedIssue)
	} else {
		a.issuesMu.Lock()
		a.selectedIssue = nil
		a.issuesMu.Unlock()
		a.updateDetailsView()
	}
	a.updateStatusBar()
}

// grouped reports whether the current sort mode uses a collapsible grouped view
// (project/milestone or label) rather than the flat sub-issue hierarchy.
func (a *App) grouped() bool {
	return a.sortField == SortByMilestone || a.sortField == SortByLabel
}

// labelGrouped reports whether the current view groups by label.
func (a *App) labelGrouped() bool {
	return a.sortField == SortByLabel
}

// buildRows builds the row model for the current mode: label-grouped, project/
// milestone-grouped (+ Seq), or the flat sub-issue hierarchy tree.
func (a *App) buildRows(issues []linearapi.Issue) ([]IssueRow, map[string]*linearapi.Issue) {
	switch {
	case a.labelGrouped():
		return BuildLabelGroupedRows(issues, a.collapsedGroups, a.hiddenStateTypes)
	case a.sortField == SortByMilestone:
		return BuildGroupedRows(issues, a.collapsedGroups, a.hiddenStateTypes)
	default:
		return BuildIssueRows(issues, a.expandedState)
	}
}

// rebuildIssuesTable rebuilds issue rows and renders the table, returning the selected issue.
func (a *App) rebuildIssuesTable(targetIssueID string) *linearapi.Issue {
	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()

	// Build rows for the current mode.
	a.issueRows, a.idToIssue = a.buildRows(issues)

	// Render table.
	renderIssuesTableModel(a.issuesTable, a.issueRows, a.idToIssue, targetIssueID, a.theme, a.grouped())

	// Select issue.
	var selectedIssue *linearapi.Issue
	if targetIssueID != "" {
		if issue, ok := a.idToIssue[targetIssueID]; ok {
			selectedIssue = issue
		}
	}

	// If no target issue, default to first available.
	if selectedIssue == nil && len(a.issueRows) > 0 {
		if issue, ok := a.idToIssue[a.issueRows[0].IssueID]; ok {
			selectedIssue = issue
		}
	}

	return selectedIssue
}

// appendIssuesData merges additional issues and updates rendered tables.
func (a *App) appendIssuesData(newIssues []linearapi.Issue) {
	if len(newIssues) == 0 {
		return
	}

	a.issuesMu.Lock()
	existing := make(map[string]bool, len(a.issues))
	for _, issue := range a.issues {
		existing[issue.ID] = true
	}
	for _, issue := range newIssues {
		if existing[issue.ID] {
			continue
		}
		a.issues = append(a.issues, issue)
		existing[issue.ID] = true
	}

	sortIssues(a.issues, a.sortField)

	targetIssueID := ""
	if a.selectedIssue != nil {
		targetIssueID = a.selectedIssue.ID
	}
	a.issuesMu.Unlock()

	selectedIssue := a.rebuildIssuesTable(targetIssueID)
	a.issuesMu.Lock()
	if selectedIssue != nil {
		a.selectedIssue = selectedIssue
	} else {
		a.selectedIssue = nil
	}
	a.issuesMu.Unlock()
	a.updateDetailsView()
	a.updateStatusBar()
}

// stateTypeOrder returns a sort rank for state types.
// Lower = sorts first. started (In Progress) first, then unstarted, triage, backlog.
func stateTypeOrder(stateType string) int {
	switch stateType {
	case "started":
		return 0
	case "unstarted":
		return 1
	case "triage":
		return 2
	case "backlog":
		return 3
	default:
		return 4
	}
}

// priorityOrder returns a normalized priority for sorting.
// Linear: 1=Urgent, 2=High, 3=Normal, 4=Low, 0=None. Map 0 to 5 so it sorts last.
func priorityOrder(p int) int {
	if p == 0 {
		return 5
	}
	return p
}

// sortIssues sorts issues based on the given sort field.
func sortIssues(issues []linearapi.Issue, field SortField) {
	switch field {
	case SortByPriority:
		sort.SliceStable(issues, func(i, j int) bool {
			return priorityOrder(issues[i].Priority) < priorityOrder(issues[j].Priority)
		})
	case SortByProjectStatus:
		sort.SliceStable(issues, func(i, j int) bool {
			a, b := issues[i], issues[j]
			// 1. Project name (empty last)
			ap, bp := a.ProjectName, b.ProjectName
			if ap == "" {
				ap = "\xff"
			}
			if bp == "" {
				bp = "\xff"
			}
			if ap != bp {
				return strings.ToLower(ap) < strings.ToLower(bp)
			}
			// 2. State type order
			sa, sb := stateTypeOrder(a.StateType), stateTypeOrder(b.StateType)
			if sa != sb {
				return sa < sb
			}
			// 3. Priority
			return priorityOrder(a.Priority) < priorityOrder(b.Priority)
		})
	case SortByStatusPriority:
		sort.SliceStable(issues, func(i, j int) bool {
			a, b := issues[i], issues[j]
			// 1. State type order
			sa, sb := stateTypeOrder(a.StateType), stateTypeOrder(b.StateType)
			if sa != sb {
				return sa < sb
			}
			// 2. Priority
			return priorityOrder(a.Priority) < priorityOrder(b.Priority)
		})
	case SortByCycle:
		sort.SliceStable(issues, func(i, j int) bool {
			a, b := issues[i], issues[j]
			// 1. Cycle name (empty last)
			ac, bc := a.CycleName, b.CycleName
			if ac == "" {
				ac = "\xff"
			}
			if bc == "" {
				bc = "\xff"
			}
			if ac != bc {
				return strings.ToLower(ac) < strings.ToLower(bc)
			}
			// 2. State type order
			sa, sb := stateTypeOrder(a.StateType), stateTypeOrder(b.StateType)
			if sa != sb {
				return sa < sb
			}
			// 3. Priority
			return priorityOrder(a.Priority) < priorityOrder(b.Priority)
		})
	case SortByMilestone:
		sort.SliceStable(issues, func(i, j int) bool {
			a, b := issues[i], issues[j]
			// 1. Project name (empty last)
			ap, bp := a.ProjectName, b.ProjectName
			if ap == "" {
				ap = "\xff"
			}
			if bp == "" {
				bp = "\xff"
			}
			if ap != bp {
				return strings.ToLower(ap) < strings.ToLower(bp)
			}
			// 2. Milestone name (no-milestone last, within the project)
			am, bm := a.MilestoneName, b.MilestoneName
			if am == "" {
				am = "\xff"
			}
			if bm == "" {
				bm = "\xff"
			}
			if am != bm {
				return strings.ToLower(am) < strings.ToLower(bm)
			}
			// 3. State type order
			sa, sb := stateTypeOrder(a.StateType), stateTypeOrder(b.StateType)
			if sa != sb {
				return sa < sb
			}
			// 4. Priority
			return priorityOrder(a.Priority) < priorityOrder(b.Priority)
		})
	}
	// For updatedAt and createdAt, the API handles sorting.
}

// onIssueSelected handles when an issue is selected.
func (a *App) onIssueSelected(issue linearapi.Issue) {
	logger.Debug("tui.app: issue selected issue=%s", issue.Identifier)
	// Set selected issue immediately for quick UI feedback
	a.issuesMu.Lock()
	a.selectedIssue = &issue
	a.issuesMu.Unlock()
	a.updateDetailsView()

	// Fetch full issue details (including comments) in background
	issueID := issue.ID
	a.fetchingIssueID = issueID

	go func() {
		logger.Debug("tui.app: fetching full issue details issue=%s", issue.Identifier)
		ctx := context.Background()
		fetchIssue := a.fetchIssueByID
		if fetchIssue == nil {
			fetchIssue = a.api.FetchIssueByID
		}
		fullIssue, err := fetchIssue(ctx, issueID)

		a.QueueUpdateDraw(func() {
			// Race-safety: only apply if this is still the issue we're fetching
			if a.fetchingIssueID == issueID {
				if err != nil {
					logger.ErrorWithErr(err, "tui.app: failed to fetch full issue details issue=%s", issue.Identifier)
					// Keep the partial issue data we already have
					return
				}
				a.issuesMu.Lock()
				a.selectedIssue = &fullIssue
				a.issuesMu.Unlock()
				a.updateDetailsView()
			}
		})
	}()
}

// toggleIssueExpanded toggles the expand/collapse state of a parent issue.
func (a *App) toggleIssueExpanded(issueID string) {
	issue, ok := a.idToIssue[issueID]
	if !ok || issue == nil {
		logger.Debug("tui.app: issue not found for toggle issue_id=%s", issueID)
		return
	}

	if len(issue.Children) == 0 {
		return
	}

	wasExpanded := a.expandedState[issueID]
	logger.Debug("tui.app: toggling issue expanded issue=%s was_expanded=%v", issue.Identifier, wasExpanded)

	ToggleExpanded(a.expandedState, issueID)

	// Rebuild rows
	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()
	a.issueRows, a.idToIssue = a.buildRows(issues)

	// Render table, selecting the toggled issue
	renderIssuesTableModel(a.issuesTable, a.issueRows, a.idToIssue, issueID, a.theme, a.grouped())
}

// enumerateGroupKeys returns the project and milestone group keys present in the
// current issue set (grouped view), used to bulk collapse/expand.
func (a *App) enumerateGroupKeys() (projKeys, msKeys []string) {
	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()

	seenP := make(map[string]bool)
	seenM := make(map[string]bool)
	for i := range issues {
		p := issues[i].ProjectName
		if p == "" {
			p = noProjectLabel
		}
		m := issues[i].MilestoneName
		if m == "" {
			m = noMilestoneLabel
		}
		pk := projectGroupKey(p)
		if !seenP[pk] {
			seenP[pk] = true
			projKeys = append(projKeys, pk)
		}
		mk := milestoneGroupKey(p, m)
		if !seenM[mk] {
			seenM[mk] = true
			msKeys = append(msKeys, mk)
		}
	}
	return projKeys, msKeys
}

// enumerateLabelKeys returns the label group keys present in the current issue set.
func (a *App) enumerateLabelKeys() []string {
	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()

	seen := make(map[string]bool)
	var keys []string
	for i := range issues {
		names := make([]string, 0, len(issues[i].Labels))
		for _, l := range issues[i].Labels {
			names = append(names, l.Name)
		}
		if len(names) == 0 {
			names = []string{noLabelLabel}
		}
		for _, n := range names {
			k := labelGroupKey(n)
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// applyFoldLevel sets the collapse state for the whole grouped view. For the
// project/milestone view: 0 = all projects collapsed, 1 = milestones collapsed,
// 2 = expanded. For the (single-level) label view: 0 = all collapsed, else
// expanded. Rebuilds and re-renders.
func (a *App) applyFoldLevel(level int) {
	a.collapsedGroups = make(map[string]bool)
	if a.labelGrouped() {
		if level == 0 {
			for _, k := range a.enumerateLabelKeys() {
				a.collapsedGroups[k] = true
			}
		}
	} else {
		projKeys, msKeys := a.enumerateGroupKeys()
		switch level {
		case 0:
			for _, k := range projKeys {
				a.collapsedGroups[k] = true
			}
		case 1:
			for _, k := range msKeys {
				a.collapsedGroups[k] = true
			}
		}
	}

	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()
	a.issueRows, a.idToIssue = a.buildRows(issues)
	renderIssuesTableModel(a.issuesTable, a.issueRows, a.idToIssue, "", a.theme, a.grouped())
	if len(a.issueRows) > 0 {
		a.issuesTable.Select(1, 0)
	}
}

// cycleFold advances the grouped-view fold level (all-collapsed → milestone
// overview → expanded). Outside grouped mode it switches into it first.
func (a *App) cycleFold() {
	if !a.grouped() {
		a.setSortField(SortByMilestone)
		return
	}
	a.foldLevel = (a.foldLevel + 1) % 3
	a.applyFoldLevel(a.foldLevel)
	a.updateStatusBar()
}

// rowModelAt returns the row model for a table row (1-based, header at row 0), or nil.
func (a *App) rowModelAt(row int) *IssueRow {
	idx := row - 1
	if idx < 0 || idx >= len(a.issueRows) {
		return nil
	}
	return &a.issueRows[idx]
}

// toggleGroupCollapse flips a project/milestone group's collapsed state and rebuilds,
// keeping the same table row selected so the cursor stays on the toggled header.
func (a *App) toggleGroupCollapse(groupKey string) {
	if groupKey == "" {
		return
	}
	selectedRow, _ := a.issuesTable.GetSelection()
	a.collapsedGroups[groupKey] = !a.collapsedGroups[groupKey]

	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()
	a.issueRows, a.idToIssue = a.buildRows(issues)
	renderIssuesTableModel(a.issuesTable, a.issueRows, a.idToIssue, "", a.theme, a.grouped())

	// Clamp selection back to the toggled header row.
	if selectedRow > len(a.issueRows) {
		selectedRow = len(a.issueRows)
	}
	if selectedRow < 1 {
		selectedRow = 1
	}
	a.issuesTable.Select(selectedRow, 0)
}

// setSearchQuery sets the search query and refreshes issues.
func (a *App) setSearchQuery(query string) {
	trimmedQuery := strings.TrimSpace(query)
	logger.Debug("tui.app: setting search query query=%s", trimmedQuery)
	a.searchQuery = trimmedQuery
	// Set focus to issues pane when searching
	a.focusedPane = FocusIssues
	a.updateFocus()
	// Run in goroutine to avoid deadlock when called from tview callbacks
	go a.refreshIssues()
}

// setSortField sets the sort field and refreshes issues.
func (a *App) setSortField(field SortField) {
	logger.Debug("tui.app: setting sort field field=%s", field)
	a.sortField = field
	// Run in goroutine to avoid deadlock when called from tview callbacks
	go a.refreshIssues()
}

// updateStatusBar updates the status bar with current information.
func (a *App) updateStatusBar() {
	var helpText string
	keyColor := a.themeTags.SecondaryText

	switch a.focusedPane {
	case FocusIssues:
		helpText = fmt.Sprintf("%sj/k: navigate | Enter/l: details | s: sort | z: fold | t: status | :: palette | /: search | q: quit[-]", keyColor)
	case FocusDetails:
		helpText = fmt.Sprintf("%sj/k: scroll | h: back | s: sort | t: status | :: palette | /: search | q: quit[-]", keyColor)
	case FocusPalette:
		helpText = fmt.Sprintf("%s↑↓: navigate | Enter: execute | Esc: close[-]", keyColor)
	default:
		helpText = fmt.Sprintf("%sj/k: navigate | Tab: switch pane | :: palette | /: search | q: quit[-]", keyColor)
	}

	searchText := ""
	if a.searchQuery != "" {
		searchText = fmt.Sprintf("%s🔍 %s[-]", a.themeTags.Warning, a.searchQuery)
	}

	sortLabel := ""
	switch a.sortField {
	case SortByProjectStatus:
		sortLabel = "project"
	case SortByStatusPriority:
		sortLabel = "status"
	case SortByUpdatedAt:
		sortLabel = "updated"
	case SortByCreatedAt:
		sortLabel = "created"
	case SortByPriority:
		sortLabel = "priority"
	case SortByCycle:
		sortLabel = "cycle"
	case SortByMilestone:
		sortLabel = "milestone"
	case SortByLabel:
		sortLabel = "label"
	}
	sortText := fmt.Sprintf("%s↕ %s[-]", a.themeTags.SecondaryText, sortLabel)

	// Build filter indicator
	filterText := ""
	hiddenCount := len(a.hiddenStateTypes)
	if hiddenCount > 0 {
		filterText = fmt.Sprintf("%s⊘ %d hidden[-]", a.themeTags.SecondaryText, hiddenCount)
	}

	a.issuesMu.RLock()
	issuesLen := len(a.issues)
	a.issuesMu.RUnlock()
	statusText := fmt.Sprintf("%s%d issues[-]", a.themeTags.Accent, issuesLen)
	if issuesLen == 0 {
		statusText = fmt.Sprintf("%sNo issues[-]", a.themeTags.SecondaryText)
	}

	sep := fmt.Sprintf("%s | [-]", a.themeTags.Border)

	parts := []string{helpText}
	if searchText != "" {
		parts = append(parts, searchText)
	}
	parts = append(parts, sortText)
	if filterText != "" {
		parts = append(parts, filterText)
	}
	parts = append(parts, statusText)

	text := parts[0]
	for i := 1; i < len(parts); i++ {
		text += sep + parts[i]
	}

	a.statusBar.SetText(text)
}

// updateStatusBarWithError updates the status bar with an error message.
func (a *App) updateStatusBarWithError(err error) {
	a.statusBar.SetText(fmt.Sprintf("%sError: %v[-]", a.themeTags.Error, err))
}

// GetAPI returns the Linear API client (used by commands).
func (a *App) GetAPI() *linearapi.Client {
	return a.api
}

// GetCache returns the team cache (used by commands).
func (a *App) GetCache() *cache.TeamCache {
	return a.cache
}

// GetSelectedIssue returns the currently selected issue.
func (a *App) GetSelectedIssue() *linearapi.Issue {
	a.issuesMu.RLock()
	defer a.issuesMu.RUnlock()
	return a.selectedIssue
}

// GetSelectedTeamID returns the currently selected team ID, if any.
func (a *App) GetSelectedTeamID() string {
	a.issuesMu.RLock()
	selectedIssue := a.selectedIssue
	a.issuesMu.RUnlock()
	if selectedIssue != nil {
		return selectedIssue.TeamID
	}
	return ""
}

// GetCurrentUser returns the current authenticated user.
func (a *App) GetCurrentUser() *linearapi.User {
	return a.currentUser
}

// GetTeamUsers returns the users for the currently selected team.
func (a *App) GetTeamUsers() []linearapi.User {
	return a.teamUsers
}

// FetchTeamUsers fetches users for a specific team from the API.
func (a *App) FetchTeamUsers(teamID string) ([]linearapi.User, error) {
	ctx := context.Background()
	users, err := a.cache.GetUsers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	a.teamUsers = users
	return users, nil
}

// GetWorkflowStates returns the workflow states for the currently selected team.
func (a *App) GetWorkflowStates() []linearapi.WorkflowState {
	return a.workflowStates
}

// QueueUpdateDraw queues a UI update function to be run in the main thread.
func (a *App) QueueUpdateDraw(f func()) {
	if a.queueUpdateDraw != nil {
		// Serialize UI updates when test overrides queueUpdateDraw to execute immediately
		a.uiUpdateMu.Lock()
		defer a.uiUpdateMu.Unlock()
		a.queueUpdateDraw(f)
		return
	}
	a.app.QueueUpdateDraw(f)
}

// loadPickerData loads picker data asynchronously if not already cached.
func (a *App) loadPickerData(
	resourceName string,
	hasData func() bool,
	loadData func(ctx context.Context, teamID string) error,
	onLoaded func(),
) {
	teamID := a.GetSelectedTeamID()
	if teamID == "" {
		logger.Warning("tui.app: cannot show %s picker, no team selected", resourceName)
		return
	}
	go func() {
		logger.Debug("tui.app: loading %s team_id=%s", resourceName, teamID)
		ctx := context.Background()
		if err := loadData(ctx, teamID); err != nil {
			logger.ErrorWithErr(err, "tui.app: failed to load %s team_id=%s", resourceName, teamID)
			a.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(err)
			})
			return
		}
		logger.Debug("tui.app: loaded %s team_id=%s", resourceName, teamID)
		a.QueueUpdateDraw(onLoaded)
	}()
}

// ShowStatusPicker shows a picker for workflow states.
func (a *App) ShowStatusPicker(onSelect func(stateID string)) {
	logger.Debug("tui.app: showing status picker")
	states := a.workflowStates
	if len(states) == 0 {
		a.loadPickerData(
			"workflow states",
			func() bool { return len(a.workflowStates) > 0 },
			func(ctx context.Context, teamID string) error {
				loadedStates, err := a.cache.GetWorkflowStates(ctx, teamID)
				if err != nil {
					return err
				}
				a.workflowStates = loadedStates
				return nil
			},
			func() {
				a.showStatusPickerWithStates(a.workflowStates, onSelect)
			},
		)
		return
	}
	a.showStatusPickerWithStates(states, onSelect)
}

func (a *App) showStatusPickerWithStates(states []linearapi.WorkflowState, onSelect func(stateID string)) {
	items := make([]PickerItem, 0, len(states))
	for _, state := range states {
		items = append(items, PickerItem{
			ID:    state.ID,
			Label: state.Name,
		})
	}

	a.pickerActive = true
	a.pickerModal.Show("Select Status", items, func(item PickerItem) {
		a.pickerActive = false
		onSelect(item.ID)
	})
}

// ShowUserPicker shows a picker for team users.
func (a *App) ShowUserPicker(onSelect func(userID string)) {
	logger.Debug("tui.app: showing user picker")
	users := a.teamUsers
	if len(users) == 0 {
		a.loadPickerData(
			"users for picker",
			func() bool { return len(a.teamUsers) > 0 },
			func(ctx context.Context, teamID string) error {
				loadedUsers, err := a.cache.GetUsers(ctx, teamID)
				if err != nil {
					return err
				}
				a.teamUsers = loadedUsers
				return nil
			},
			func() {
				a.showUserPickerWithUsers(a.teamUsers, onSelect)
			},
		)
		return
	}
	a.showUserPickerWithUsers(users, onSelect)
}

func (a *App) showUserPickerWithUsers(users []linearapi.User, onSelect func(userID string)) {
	items := make([]PickerItem, 0, len(users))
	for _, user := range users {
		label := user.Name
		if user.IsMe {
			label += " (me)"
		}
		items = append(items, PickerItem{
			ID:    user.ID,
			Label: label,
		})
	}

	a.pickerActive = true
	a.pickerModal.Show("Select Assignee", items, func(item PickerItem) {
		a.pickerActive = false
		onSelect(item.ID)
	})
}

// stateTypeLabels maps state types to human-readable labels.
var stateTypeLabels = map[string]string{
	"started":   "In Progress",
	"unstarted": "Todo",
	"triage":    "Triage",
	"backlog":   "Backlog",
	"completed": "Done",
	"canceled":  "Canceled",
	"duplicate": "Duplicate",
}

// stateTypeOrder is the display order for state types in the filter picker.
var stateTypePickerOrder = []string{"started", "unstarted", "triage", "backlog", "completed", "canceled", "duplicate"}

// buildStateTypePickerItems builds the picker items reflecting current filter state.
func (a *App) buildStateTypePickerItems() []PickerItem {
	items := make([]PickerItem, 0, len(stateTypePickerOrder))
	for _, st := range stateTypePickerOrder {
		label := stateTypeLabels[st]
		if a.hiddenStateTypes[st] {
			label = "[ ] " + label
		} else {
			label = "[✓] " + label
		}
		items = append(items, PickerItem{
			ID:    st,
			Label: label,
		})
	}
	return items
}

// showStateTypeFilterPicker shows a picker to toggle which state types are visible.
func (a *App) showStateTypeFilterPicker() {
	a.pickerActive = true
	a.pickerModal.ShowToggle(
		"Toggle Status Filter",
		a.buildStateTypePickerItems(),
		func(item PickerItem) {
			// Toggle the state type
			if a.hiddenStateTypes[item.ID] {
				delete(a.hiddenStateTypes, item.ID)
			} else {
				a.hiddenStateTypes[item.ID] = true
			}
			// Update the picker items in place to show new checkmarks
			a.pickerModal.UpdateItems(a.buildStateTypePickerItems())
		},
		func() {
			// On dismiss, refresh issues with new filters
			a.pickerActive = false
			go a.refreshIssues()
		},
	)
}

// ShowParentIssuePicker shows a picker for selecting a parent issue.
// It lists all top-level issues (issues without a parent) from the current list.
func (a *App) ShowParentIssuePicker(onSelect func(parentID string)) {
	// Filter to only show issues that could be parents (no parent themselves)
	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()
	items := make([]PickerItem, 0)
	for _, issue := range issues {
		if issue.Parent == nil {
			items = append(items, PickerItem{
				ID:    issue.ID,
				Label: issue.Identifier + " - " + issue.Title,
			})
		}
	}

	if len(items) == 0 {
		logger.Warning("tui.app: no parent issues available for picker")
		a.updateStatusBarWithError(fmt.Errorf("no parent issues available"))
		return
	}
	logger.Debug("tui.app: parent issue picker items count=%d", len(items))

	a.pickerActive = true
	a.pickerModal.Show("Select Parent Issue", items, func(item PickerItem) {
		a.pickerActive = false
		onSelect(item.ID)
	})
}

// ShowCreateIssueModal shows the create issue modal.
func (a *App) ShowCreateIssueModal() {
	a.showCreateIssueModalWithParent("")
}

// ShowCreateSubIssueModal shows the create issue modal with a parent issue pre-set.
func (a *App) ShowCreateSubIssueModal(parentID string) {
	a.showCreateIssueModalWithParent(parentID)
}

// showCreateIssueModalWithParent shows the create issue modal, optionally with a parent.
func (a *App) showCreateIssueModalWithParent(parentID string) {
	teamID := a.GetSelectedTeamID()
	projectID := ""

	a.createIssueModal.Show(teamID, projectID, func(title, description, tID, pID, assigneeID string, priority int) {
		if title == "" {
			return
		}
		go func() {
			ctx := context.Background()
			input := linearapi.CreateIssueInput{
				TeamID:      tID,
				Title:       title,
				Description: description,
			}
			if pID != "" {
				input.ProjectID = pID
			}
			if assigneeID != "" {
				input.AssigneeID = assigneeID
			}
			if priority > 0 {
				input.Priority = priority
			}
			if parentID != "" {
				input.ParentID = parentID
			}
			issue, err := a.api.CreateIssue(ctx, input)
			a.QueueUpdateDraw(func() {
				if err != nil {
					logger.ErrorWithErr(err, "tui.app: failed to create issue title=%s", title)
					a.updateStatusBarWithError(err)
					return
				}
				if parentID != "" {
					logger.Info("tui.app: created sub-issue issue=%s title=%s", issue.Identifier, title)
				} else {
					logger.Info("tui.app: created issue issue=%s title=%s", issue.Identifier, title)
				}
				go a.refreshIssues(issue.ID)
			})
		}()
	})
}

// ShowEditTitleModal shows the edit title modal.
func (a *App) ShowEditTitleModal() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		return
	}

	a.editTitleModal.Show(issue.ID, issue.Title, func(issueID, title string) {
		go func() {
			ctx := context.Background()
			_, err := a.api.UpdateIssue(ctx, linearapi.UpdateIssueInput{
				ID:    issueID,
				Title: &title,
			})
			a.QueueUpdateDraw(func() {
				if err != nil {
					logger.ErrorWithErr(err, "tui.app: failed to update issue title issue=%s", issue.Identifier)
					a.updateStatusBarWithError(err)
					return
				}
				logger.Info("tui.app: updated issue title issue=%s", issue.Identifier)
				go a.refreshIssues(issueID)
			})
		}()
	})
}

// ShowEditLabelsModal shows the edit labels modal for the selected issue.
func (a *App) ShowEditLabelsModal() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		return
	}

	teamID := issue.TeamID
	if teamID == "" {
		teamID = a.GetSelectedTeamID()
	}
	if teamID == "" {
		logger.Warning("tui.app: cannot edit labels, no team context issue=%s", issue.Identifier)
		a.updateStatusBarWithError(fmt.Errorf("cannot edit labels: no team context"))
		return
	}

	// Get current label IDs from the issue
	currentLabelIDs := make([]string, len(issue.Labels))
	for i, lbl := range issue.Labels {
		currentLabelIDs[i] = lbl.ID
	}

	// Load available labels asynchronously
	go func() {
		logger.Debug("tui.app: loading labels for edit modal issue=%s team_id=%s", issue.Identifier, teamID)
		ctx := context.Background()
		availableLabels, err := a.cache.GetIssueLabels(ctx, teamID)
		if err != nil {
			logger.ErrorWithErr(err, "tui.app: failed to load labels issue=%s team_id=%s", issue.Identifier, teamID)
			a.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(err)
			})
			return
		}
		logger.Debug("tui.app: loaded labels issue=%s count=%d", issue.Identifier, len(availableLabels))

		a.QueueUpdateDraw(func() {
			a.editLabelsModal.Show(issue.ID, currentLabelIDs, availableLabels, func(issueID string, labelIDs []string) {
				go func() {
					ctx := context.Background()
					_, err := a.api.UpdateIssue(ctx, linearapi.UpdateIssueInput{
						ID:       issueID,
						LabelIDs: &labelIDs,
					})
					a.QueueUpdateDraw(func() {
						if err != nil {
							logger.ErrorWithErr(err, "tui.app: failed to update labels issue=%s", issue.Identifier)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("tui.app: updated labels issue=%s", issue.Identifier)
						go a.refreshIssues(issueID)
					})
				}()
			})
		})
	}()
}

// ShowSettingsModal shows the settings modal.
func (a *App) ShowSettingsModal() {
	if a.settingsModal == nil {
		return
	}

	a.settingsModal.Show()
}

// ShowPromptTemplatesModal shows the prompt templates modal.
func (a *App) ShowPromptTemplatesModal() {
	if a.promptTemplatesModal == nil {
		return
	}

	promptsPath, err := config.PromptTemplatesFilePath()
	if err != nil {
		a.updateStatusBarWithError(err)
		return
	}

	templates, err := config.EnsurePromptTemplatesFile(promptsPath)
	if err != nil {
		a.updateStatusBarWithError(err)
		templates = a.agentPromptTemplates
		if len(templates) == 0 {
			templates = config.DefaultAgentPromptTemplates()
		}
	} else {
		a.agentPromptTemplates = templates
	}

	a.promptTemplatesModal.Show(templates, func(updated []config.AgentPromptTemplate) error {
		if err := config.SavePromptTemplates(promptsPath, updated); err != nil {
			return err
		}
		a.agentPromptTemplates = updated
		a.agentPromptModal = NewAgentPromptModal(a)
		return nil
	})
}
