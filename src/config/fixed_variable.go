package variable

import (
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/utils"

	"github.com/adrg/xdg"
)

const (
	CurrentVersion = "v1.6.0"
	// Allowing pre-releases with non production version
	// Set this to "" for production releases
	PreReleaseSuffix = ""

	// This gives most recent non-prerelease, non-draft release
	LatestVersionURL    = "https://api.github.com/repos/beyondmarks/wrapper/releases/latest"
	LatestVersionGithub = "github.com/beyondmarks-ai/Wrapper/releases/latest"

	// This will not break in windows. This is a relative path for Embed FS. It uses "/" only
	EmbedConfigDir           = "src/wrapper_config"
	EmbedConfigFile          = EmbedConfigDir + "/config.toml"
	EmbedHotkeysFile         = EmbedConfigDir + "/hotkeys.toml"
	EmbedThemeDir            = EmbedConfigDir + "/theme"
	EmbedThemeCatppuccinFile = EmbedThemeDir + "/catppuccin-mocha.toml"
)

var (
	HomeDir         = xdg.Home
	WrapperMainDir  = filepath.Join(xdg.ConfigHome, "wrapper")
	WrapperCacheDir = filepath.Join(xdg.CacheHome, "wrapper")
	WrapperDataDir  = filepath.Join(xdg.DataHome, "wrapper")
	WrapperStateDir = filepath.Join(xdg.StateHome, "wrapper")

	// MainDir files
	ThemeFolder = filepath.Join(WrapperMainDir, "theme")

	// DataDir files
	LastCheckVersion  = filepath.Join(WrapperDataDir, "lastCheckVersion")
	ThemeFileVersion  = filepath.Join(WrapperDataDir, "themeFileVersion")
	FirstUseCheck     = filepath.Join(WrapperDataDir, "firstUseCheck")
	PinnedFile        = filepath.Join(WrapperDataDir, "pinned.json")
	ToggleDotFile     = filepath.Join(WrapperDataDir, "toggleDotFile")
	ToggleFooter      = filepath.Join(WrapperDataDir, "toggleFooter")
	CloudStateFile    = filepath.Join(WrapperDataDir, "cloud.json")
	CloudIdentityFile = filepath.Join(WrapperDataDir, "cloud-identity.bin")
	CloudTokensFile   = filepath.Join(WrapperDataDir, "cloud-tokens.bin")
	CloudCacheDir     = filepath.Join(WrapperCacheDir, "transfers")

	// StateDir files
	LogFile     = filepath.Join(WrapperStateDir, "wrapper.log")
	LastDirFile = filepath.Join(WrapperStateDir, "lastdir")

	// Trash Directories
	DarwinTrashDirectory = filepath.Join(HomeDir, ".Trash")

	// Linux home trash directory paths used for sidebar display and tests.
	LinuxTrashDirectory      = filepath.Join(xdg.DataHome, "Trash")
	LinuxTrashDirectoryFiles = filepath.Join(xdg.DataHome, "Trash", "files")
	LinuxTrashDirectoryInfo  = filepath.Join(xdg.DataHome, "Trash", "info")
)

// These variables are actually not fixed, they are sometimes updated dynamically
var (
	ConfigFile  = filepath.Join(WrapperMainDir, "config.toml")
	HotkeysFile = filepath.Join(WrapperMainDir, "hotkeys.toml")

	// ChooserFile is the path where wrapper will write the file's path, which is to be
	// opened, before exiting
	ChooserFile = ""

	// Other state variables
	FixHotkeys    = false
	FixConfigFile = false
	LastDir       = ""
	PrintLastDir  = false
)

// Still we are preventing other packages to directly modify them via reassign linter

func SetLastDir(path string) {
	LastDir = path
}

func SetChooserFile(path string) {
	ChooserFile = path
}

func UpdateVarFromCliArgs(c *cli.Command) {
	// Setting the config file path
	configFileArg := c.String("config-file")

	// Validate the config file exists
	if configFileArg != "" {
		if _, err := os.Stat(configFileArg); err != nil {
			utils.PrintfAndExitf("Error: While reading config file '%s' from argument : %v", configFileArg, err)
		}
		ConfigFile = configFileArg
	}

	hotkeyFileArg := c.String("hotkey-file")

	if hotkeyFileArg != "" {
		if _, err := os.Stat(hotkeyFileArg); err != nil {
			utils.PrintfAndExitf("Error: While reading hotkey file '%s' from argument : %v", hotkeyFileArg, err)
		}
		HotkeysFile = hotkeyFileArg
	}

	// It could be non existent. We are writing to the file. If file doesn't exists, we would attempt to create it.
	SetChooserFile(c.String("chooser-file"))

	FixHotkeys = c.Bool("fix-hotkeys")
	FixConfigFile = c.Bool("fix-config-file")
	PrintLastDir = c.Bool("print-last-dir")
}
