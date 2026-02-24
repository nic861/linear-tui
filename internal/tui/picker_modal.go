package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PickerItem represents an item in a picker.
type PickerItem struct {
	ID    string
	Label string
}

// PickerModal manages a picker overlay for selecting from a list of items.
type PickerModal struct {
	app        *App
	modal      *tview.Flex
	list       *tview.List
	titleView  *tview.TextView
	helpText   *tview.TextView
	items      []PickerItem
	onSelect   func(item PickerItem)
	onToggle   func(item PickerItem)    // Called on Space in toggle mode
	onDismiss  func()                    // Called on Enter/Esc in toggle mode
	toggleMode bool
}

// NewPickerModal creates a new picker modal.
func NewPickerModal(app *App) *PickerModal {
	pm := &PickerModal{
		app: app,
	}

	// Create list
	pm.list = tview.NewList().
		ShowSecondaryText(false).
		SetMainTextColor(app.theme.Foreground).
		SetSelectedBackgroundColor(app.theme.Accent).
		SetSelectedTextColor(app.theme.SelectionText).
		SetHighlightFullLine(true)
	pm.list.SetBackgroundColor(app.theme.HeaderBg)

	// Create title
	pm.titleView = tview.NewTextView()
	pm.titleView.SetTextColor(app.theme.Accent)
	pm.titleView.SetBackgroundColor(app.theme.HeaderBg)

	// Create help text
	pm.helpText = tview.NewTextView()
	pm.helpText.SetText("↑↓/j/k: navigate | Enter: select | Esc: cancel")
	pm.helpText.SetTextColor(app.theme.SecondaryText)
	pm.helpText.SetBackgroundColor(app.theme.HeaderBg)

	// Build modal content
	modalContent := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(pm.titleView, 1, 0, false).
		AddItem(pm.list, 0, 1, true).
		AddItem(pm.helpText, 1, 0, false)
	modalContent.Box = tview.NewBox().SetBackgroundColor(app.theme.HeaderBg)
	modalContent.SetBackgroundColor(app.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(app.theme.Accent).
		SetTitleColor(app.theme.Foreground)
	padding := app.density.ModalPadding
	modalContent.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	// Center the modal on screen
	pm.modal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(modalContent, 15, 0, true).
			AddItem(nil, 0, 1, false), 50, 0, true).
		AddItem(nil, 0, 1, false)
	pm.modal.SetBackgroundColor(app.theme.Background)

	return pm
}

// Show displays the picker modal with the given title and items.
func (pm *PickerModal) Show(title string, items []PickerItem, onSelect func(item PickerItem)) {
	pm.items = items
	pm.onSelect = onSelect
	pm.onToggle = nil
	pm.onDismiss = nil
	pm.toggleMode = false
	pm.helpText.SetText("↑↓/j/k: navigate | Enter: select | Esc: cancel")

	pm.titleView.SetText(title)
	pm.list.Clear()

	for _, item := range items {
		pm.list.AddItem(item.Label, "", 0, nil)
	}

	if len(items) > 0 {
		pm.list.SetCurrentItem(0)
	}

	pm.app.pages.AddPage("picker", pm.modal, true, true)
	pm.app.pages.SendToFront("picker")
	pm.app.app.SetFocus(pm.list)
}

// ShowToggle displays the picker in toggle mode where Space toggles items and Enter closes.
func (pm *PickerModal) ShowToggle(title string, items []PickerItem, onToggle func(item PickerItem), onDismiss func()) {
	pm.items = items
	pm.onSelect = nil
	pm.onToggle = onToggle
	pm.onDismiss = onDismiss
	pm.toggleMode = true
	pm.helpText.SetText("↑↓/j/k: navigate | Space: toggle | Enter: done")

	pm.titleView.SetText(title)
	pm.list.Clear()

	for _, item := range items {
		pm.list.AddItem(item.Label, "", 0, nil)
	}

	if len(items) > 0 {
		pm.list.SetCurrentItem(0)
	}

	pm.app.pages.AddPage("picker", pm.modal, true, true)
	pm.app.pages.SendToFront("picker")
	pm.app.app.SetFocus(pm.list)
}

// UpdateItems refreshes the picker list items (used during toggle mode).
func (pm *PickerModal) UpdateItems(items []PickerItem) {
	idx := pm.list.GetCurrentItem()
	pm.items = items
	pm.list.Clear()
	for _, item := range items {
		pm.list.AddItem(item.Label, "", 0, nil)
	}
	if idx >= 0 && idx < len(items) {
		pm.list.SetCurrentItem(idx)
	}
}

// Hide hides the picker modal.
func (pm *PickerModal) Hide() {
	pm.app.pickerActive = false
	pm.app.pages.RemovePage("picker")

	// Check if create issue modal is visible and restore focus to it
	if pm.app.pages.HasPage("create_issue") {
		pm.app.pages.SendToFront("create_issue")
		if pm.app.createIssueModal != nil {
			pm.app.app.SetFocus(pm.app.createIssueModal.form)
		}
		return
	}

	// Check if edit title modal is visible and restore focus to it
	if pm.app.pages.HasPage("edit_title") {
		pm.app.pages.SendToFront("edit_title")
		if pm.app.editTitleModal != nil {
			pm.app.app.SetFocus(pm.app.editTitleModal.form)
		}
		return
	}

	pm.app.updateFocus()
}

// HandleKey handles keyboard input for the picker.
func (pm *PickerModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		pm.Hide()
		if pm.toggleMode && pm.onDismiss != nil {
			pm.onDismiss()
		}
		return nil
	case tcell.KeyEnter:
		if pm.toggleMode {
			// In toggle mode, Enter closes the dialog
			pm.Hide()
			if pm.onDismiss != nil {
				pm.onDismiss()
			}
			return nil
		}
		// In select mode, Enter picks the item
		idx := pm.list.GetCurrentItem()
		if idx >= 0 && idx < len(pm.items) {
			item := pm.items[idx]
			pm.Hide()
			if pm.onSelect != nil {
				pm.onSelect(item)
			}
		}
		return nil
	case tcell.KeyUp:
		idx := pm.list.GetCurrentItem()
		if idx > 0 {
			pm.list.SetCurrentItem(idx - 1)
		}
		return nil
	case tcell.KeyDown:
		idx := pm.list.GetCurrentItem()
		if idx < pm.list.GetItemCount()-1 {
			pm.list.SetCurrentItem(idx + 1)
		}
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case ' ':
			if pm.toggleMode && pm.onToggle != nil {
				idx := pm.list.GetCurrentItem()
				if idx >= 0 && idx < len(pm.items) {
					pm.onToggle(pm.items[idx])
				}
			}
			return nil
		case 'j':
			idx := pm.list.GetCurrentItem()
			if idx < pm.list.GetItemCount()-1 {
				pm.list.SetCurrentItem(idx + 1)
			}
			return nil
		case 'k':
			idx := pm.list.GetCurrentItem()
			if idx > 0 {
				pm.list.SetCurrentItem(idx - 1)
			}
			return nil
		}
	}
	return event
}

// GetModal returns the modal flex for adding to pages.
func (pm *PickerModal) GetModal() *tview.Flex {
	return pm.modal
}
