package internal

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestWelcomeRenderUsesLargeASCIIAtNormalSize(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	m.welcomeOpen = true
	m.fullWidth = 100
	m.fullHeight = 30

	rendered := m.welcomeRender()
	assert.Contains(t, rendered, "B E Y O N D   M A R K S   P R E S E N T S")
	assert.Contains(t, rendered, "██╗    ██╗   ██████╗")
	assert.Contains(t, rendered, "SEARCH. ORGANIZE. CONNECT.")
	assert.Contains(t, rendered, "ENTER")
	assert.Contains(t, rendered, "ESC")
}

func TestWelcomeRenderUsesMediumASCIIWhenLargeDoesNotFit(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	m.welcomeOpen = true
	m.fullWidth = 70
	m.fullHeight = 20

	rendered := m.welcomeRender()
	assert.Contains(t, rendered, "__        ______  ___")
	assert.NotContains(t, rendered, "██╗    ██╗")
}

func TestWelcomeRenderUsesCompactBrandAtSmallSize(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	m.welcomeOpen = true
	m.fullWidth = 40
	m.fullHeight = 10

	rendered := m.welcomeRender()
	assert.Contains(t, rendered, "W R A P P E R")
	assert.NotContains(t, rendered, "__        ______  ___")
}

func TestWelcomeInput(t *testing.T) {
	t.Run("enter continues", func(t *testing.T) {
		m := defaultTestModel(t.TempDir())
		m.welcomeOpen = true

		cmd := m.handleWelcomeKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		assert.False(t, m.welcomeOpen)
		assert.Nil(t, cmd)
	})

	t.Run("escape quits", func(t *testing.T) {
		m := defaultTestModel(t.TempDir())
		m.welcomeOpen = true

		cmd := m.handleWelcomeKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		assert.True(t, m.welcomeOpen)
		assert.True(t, IsTeaQuit(cmd))
	})

	t.Run("other keys are ignored", func(t *testing.T) {
		m := defaultTestModel(t.TempDir())
		m.welcomeOpen = true

		cmd := m.handleWelcomeKey(tea.KeyPressMsg{Code: 'j'})
		assert.True(t, m.welcomeOpen)
		assert.Nil(t, cmd)
	})
}
