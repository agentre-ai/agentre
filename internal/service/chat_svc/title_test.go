package chat_svc

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionTitleFromFirstMessagePreservesTextBeyondVisualClamp(t *testing.T) {
	text := "Optimize Edit/Write/file_change so the frontend owns visual truncation"
	require.Greater(t, utf8.RuneCountInString(text), 30)

	got := sessionTitleFromFirstMessage("  " + text + "  ")

	assert.Equal(t, text, got)
	assert.NotContains(t, got, "\u2026")
}

func TestSessionTitleFromFirstMessageDoesNotApplyRenameLimit(t *testing.T) {
	text := strings.Repeat("x", renameTitleMaxRunes+1)

	got := sessionTitleFromFirstMessage(text)

	assert.Equal(t, text, got)
	assert.NotContains(t, got, "\u2026")
}
