package internal

import (
	zoxidelib "github.com/lazysegtree/go-zoxide"

	"github.com/atotto/clipboard"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/helpmenu"

	everythingui "github.com/beyondmarks-ai/Wrapper/src/internal/ui/everything"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/filemodel"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/sortmodel"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/metadata"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/processbar"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/sidebar"

	"github.com/beyondmarks-ai/Wrapper/src/internal/common"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/prompt"
	zoxideui "github.com/beyondmarks-ai/Wrapper/src/internal/ui/zoxide"
)

// Generate and return model containing default configurations for interface
// Maybe we can replace slice of strings with var args - Should we ?
// TODO: Move the configuration parameters to a ModelConfig struct.
// Something like `RendererConfig` struct for `Renderer` struct in ui/renderer package
// Or even better API like varargs lambda function opts
// which can be WithFooter(), WithXYZ()
// Lots of improvements are waiting on it
//   - Allow Sending thumbnailGeneratorNeeded as false to preview.New()
//     to prevent noise in test logs. Same with imagePreviewer
func defaultModelConfig(toggleDotFile, toggleFooter, firstUse bool,
	firstPanelPaths []string, zClient *zoxidelib.Client) *model {
	return &model{
		focusPanel:      nonePanelFocus,
		processBarModel: processbar.New(),
		clipboardWriter: clipboard.WriteAll,
		sidebarModel:    sidebar.New(),
		fileMetaData:    metadata.New(),
		fileModel:       filemodel.New(firstPanelPaths, toggleDotFile),
		helpMenu:        helpmenu.New(),
		promptModal:     prompt.DefaultModel(prompt.PromptMinHeight, prompt.PromptMinWidth),
		zoxideModal:     zoxideui.DefaultModel(zoxideui.ZoxideMinHeight, zoxideui.ZoxideMinWidth, zClient),
		everythingModal: everythingui.DefaultModel(everythingui.MinHeight, everythingui.MinWidth),
		sortModal:       sortmodel.New(),
		zClient:         zClient,
		modelQuitState:  notQuitting,
		toggleFooter:    toggleFooter,
		firstUse:        firstUse,
		welcomeOpen:     true,
		hasTrash:        common.InitTrash(),
	}
}
