package internal

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const welcomeLargeASCII = `██╗    ██╗   ██████╗    █████╗   ██████╗   ██████╗   ███████╗  ██████╗
██║    ██║   ██╔══██╗  ██╔══██╗  ██╔══██╗  ██╔══██╗  ██╔════╝  ██╔══██╗
██║ █╗ ██║   ██████╔╝  ███████║  ██████╔╝  ██████╔╝  █████╗    ██████╔╝
██║███╗██║   ██╔══██╗  ██╔══██║  ██╔═══╝   ██╔═══╝   ██╔══╝    ██╔══██╗
╚███╔███╔╝   ██║  ██║  ██║  ██║  ██║       ██║       ███████╗  ██║  ██║
 ╚══╝╚══╝    ╚═╝  ╚═╝  ╚═╝  ╚═╝  ╚═╝       ╚═╝       ╚══════╝  ╚═╝  ╚═╝`

const welcomeMediumASCII = `__        ______  ___    ____  ____  __________
\ \      / / __ \/   |  / __ \/ __ \/ ____/ __ \
 \ \ /\ / / /_/ / /| | / /_/ / /_/ / __/ / /_/ /
  \ V  V / _, _/ ___ |/ ____/ ____/ /___/ _, _/
   \_/\_/_/ |_/_/  |_/_/   /_/   /_____/_/ |_|`

const welcomeGreen = "#63D95B"

func (m *model) handleWelcomeKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.Code {
	case tea.KeyEnter:
		m.welcomeOpen = false
	case tea.KeyEscape:
		return tea.Quit
	}
	return nil
}

func (m *model) welcomeRender() string {
	green := lipgloss.Color(welcomeGreen)
	title := lipgloss.NewStyle().
		Foreground(green).
		Render("B E Y O N D   M A R K S   P R E S E N T S")

	brandText := welcomeLargeASCII
	if m.fullWidth < lipgloss.Width(welcomeLargeASCII)+4 || m.fullHeight < 15 {
		brandText = welcomeMediumASCII
	}
	if m.fullWidth < lipgloss.Width(welcomeMediumASCII)+4 || m.fullHeight < 13 {
		brandText = "W R A P P E R"
	}
	brand := lipgloss.NewStyle().
		Foreground(lipgloss.BrightGreen).
		Bold(true).
		Render(brandText)

	taglineText := "-----  SEARCH. ORGANIZE. CONNECT.  -----"
	if m.fullWidth < len(taglineText)+4 {
		taglineText = "SEARCH. ORGANIZE. CONNECT."
	}
	tagline := lipgloss.NewStyle().
		Foreground(green).
		Render(taglineText)

	controls := welcomeHint(min(max(m.fullWidth-2, 1), 40))
	content := lipgloss.JoinVertical(lipgloss.Center,
		title,
		"",
		brand,
		"",
		tagline,
		"",
		controls,
	)

	return lipgloss.NewStyle().
		Width(m.fullWidth).
		Height(m.fullHeight).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}

func welcomeHint(width int) string {
	label := "[ ENTER ] Continue    [ ESC ] Exit"
	if width < len(label) {
		label = "ENTER Continue  |  ESC Exit"
	}
	if width < len(label) {
		label = "ENTER  |  ESC"
	}
	if width < len(label) {
		label = ""
	}
	return lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Foreground(lipgloss.BrightGreen).
		Render(label)
}
