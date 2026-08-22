package internal

import (
	"sync"

	zoxidelib "github.com/lazysegtree/go-zoxide"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/helpmenu"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/wraperror"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/clipboard"
	everythingui "github.com/beyondmarks-ai/Wrapper/src/internal/ui/everything"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/sortmodel"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/filemodel"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/metadata"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/notify"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/processbar"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/sidebar"

	"charm.land/bubbles/v2/textinput"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/prompt"
	zoxideui "github.com/beyondmarks-ai/Wrapper/src/internal/ui/zoxide"
)

// Type representing the type of focused panel
type focusPanelType int

type modelQuitStateType int

// Constants for panel with no focus
const (
	nonePanelFocus focusPanelType = iota
	processBarFocus
	sidebarFocus
	metadataFocus
)

const (
	notQuitting modelQuitStateType = iota
	quitInitiated
	quitConfirmationInitiated
	quitConfirmationReceived
	quitDone
)

// Main model
// TODO : We could consider using *model as tea.Model, instead of model.
// for reducing re-allocations. The struct is 20K bytes. But this could lead to
// issues like race conditions and whatnot, which are hidden since we are creating
// new model in each tea update.
type model struct {
	// Main Panels
	fileModel       filemodel.Model
	sidebarModel    sidebar.Model
	processBarModel processbar.Model
	clipboard       clipboard.Model
	clipboardWriter func(string) error
	focusPanel      focusPanelType

	// Modals
	notifyModel     notify.Model
	typingModal     typingModal
	helpMenu        helpmenu.Model
	promptModal     prompt.Model
	zoxideModal     zoxideui.Model
	everythingModal everythingui.Model
	sortModal       sortmodel.Model
	wrapError       wraperror.Model
	mutexErrorModal sync.Mutex

	// Zoxide client for directory tracking
	zClient *zoxidelib.Client

	fileMetaData metadata.Model

	// no use directly for increment, use nextIoReqCnt
	ioReqCnt int32

	modelQuitState       modelQuitStateType
	firstTextInput       bool
	toggleFooter         bool
	firstLoadingComplete bool
	firstUse             bool
	welcomeOpen          bool

	// This entirely disables metadata fetching. Used in test model
	disableMetadata bool

	// Height in number of lines of actual viewport of
	// main panel and sidebar excluding border
	mainPanelHeight int

	// Height in number of lines of actual viewport of
	// footer panels - process/metadata/clipboard - excluding border
	footerHeight int
	fullWidth    int
	fullHeight   int

	// whether usable trash directory exists or not
	hasTrash bool
}

type typingModal struct {
	location  string
	open      bool
	textInput textinput.Model
}

type editorFinishedMsg struct{ err error }
