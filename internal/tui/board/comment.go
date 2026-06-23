package board

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

// newCommentTextarea builds a fresh focused textarea sized for the modal.
// Width comes from commentTextareaWidth so the modal stays inside the
// frame border lipgloss draws around it.
func newCommentTextarea(width int) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type your comment. Markdown is supported."
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(width)
	ta.SetHeight(6)
	ta.Focus()
	return ta
}

// commentTextareaWidth gives the textarea its inner width given the
// overall terminal width. Keeps the modal pleasant on both 80- and
// 200-column terminals.
func commentTextareaWidth(termWidth int) int {
	w := termWidth - 8
	if w < 40 {
		w = 40
	}
	if w > 100 {
		w = 100
	}
	return w
}

// renderCommentModal wraps the textarea in a bordered panel with a title
// and a footer hint. Sits in the middle of the screen via lipgloss.Place
// in View.
func renderCommentModal(area string) string {
	title := titleStyle.Render("Add comment")
	hint := dimStyle.Render("ctrl+s submit  ·  esc cancel")
	body := title + "\n\n" + area + "\n\n" + hint
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1, 2).
		Render(body)
}
