package board

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sn3d/tm/internal/client"
)

// renderDetail builds the full-screen detail view. Width is the terminal
// column count; scrollOffset is the row index of the first visible content
// row (the header always stays anchored at the top — only the body
// scrolls). The whole block is returned as a string so View can append
// footer / overlay.
func renderDetail(d TaskDetail, resolver interface {
	StateText(client.TaskState) string
	LabelsText([]string) string
}, width, height, scrollOffset int) string {
	if width <= 0 {
		width = 100
	}
	header := detailHeader(d.Task, resolver, width)
	body := detailBody(d, resolver, width)

	// Apply the vertical scroll to the body only — header is fixed.
	bodyLines := strings.Split(body, "\n")
	if scrollOffset > len(bodyLines) {
		scrollOffset = len(bodyLines)
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	visible := bodyLines[scrollOffset:]
	// Cap to remaining vertical space so the footer rendered by View
	// always lands at the bottom and the help modal can overlay
	// cleanly.
	maxBodyLines := height - lipgloss.Height(header) - 2 // 2 = footer + spacer
	if maxBodyLines > 0 && len(visible) > maxBodyLines {
		visible = visible[:maxBodyLines]
	}
	return header + "\n" + strings.Join(visible, "\n")
}

// detailHeader renders the always-visible top: ID + subject + state /
// agent / labels in a one-line summary, then a horizontal rule.
func detailHeader(t client.Task, resolver interface {
	StateText(client.TaskState) string
	LabelsText([]string) string
}, width int) string {
	id := idStyle.Render("#" + t.ID)
	subject := titleStyle.Render(t.Subject)
	rule := dimStyle.Render(strings.Repeat("─", maxInt(1, width-2)))

	meta := []string{resolver.StateText(t.State)}
	if t.AssignedAgent != "" {
		meta = append(meta, dimStyle.Render("@")+dimStyle.Render(t.AssignedAgent))
	} else {
		meta = append(meta, dimStyle.Render("—"))
	}
	if len(t.Labels) > 0 {
		meta = append(meta, resolver.LabelsText(t.Labels))
	}
	if t.ParentID != "" {
		meta = append(meta, dimStyle.Render("parent ")+idStyle.Render("#"+t.ParentID))
	}
	if t.ArchivedAt != nil {
		meta = append(meta, lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("archived"))
	}

	line1 := id + "  " + subject
	line2 := dimStyle.Render("│ ") + strings.Join(meta, dimStyle.Render(" · "))
	return line1 + "\n" + line2 + "\n" + rule
}

// detailBody renders the description, subtasks and comments sections of
// the detail view. Returned newline-joined; renderDetail handles
// scroll-window slicing.
func detailBody(d TaskDetail, resolver interface {
	StateText(client.TaskState) string
	LabelsText([]string) string
}, width int) string {
	var b strings.Builder

	// Description
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Description"))
	b.WriteString("\n")
	if strings.TrimSpace(d.Task.Description) == "" {
		b.WriteString(dimStyle.Render("(none)"))
		b.WriteString("\n")
	} else {
		b.WriteString(wrapText(d.Task.Description, width-2))
		b.WriteString("\n")
	}

	// Subtasks
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf("Subtasks (%d)", len(d.Subtasks))))
	b.WriteString("\n")
	if len(d.Subtasks) == 0 {
		b.WriteString(dimStyle.Render("(none)"))
		b.WriteString("\n")
	} else {
		for _, st := range d.Subtasks {
			id := idStyle.Render("#" + st.ID)
			agent := dimStyle.Render("—")
			if st.AssignedAgent != "" {
				agent = dimStyle.Render("@") + dimStyle.Render(st.AssignedAgent)
			}
			line := fmt.Sprintf("  %s  %s  %s  %s",
				id, truncate(st.Subject, maxInt(20, width/2)), resolver.StateText(st.State), agent)
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Comments
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf("Comments (%d)", len(d.Comments))))
	b.WriteString("\n")
	if len(d.Comments) == 0 {
		b.WriteString(dimStyle.Render("(none)"))
		b.WriteString("\n")
	} else {
		for _, c := range d.Comments {
			who := lipgloss.NewStyle().Bold(true).Render(c.Who)
			b.WriteString(dimStyle.Render("• "))
			b.WriteString(who)
			b.WriteString("\n")
			b.WriteString(wrapText(c.Comment, width-4))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// wrapText soft-wraps s at width cells, preserving existing newlines.
// Long words that exceed width are NOT broken — they overflow gracefully
// so the user still sees the URL or identifier in full.
func wrapText(s string, width int) string {
	if width <= 1 {
		return s
	}
	var out []string
	for _, paragraph := range strings.Split(s, "\n") {
		if lipgloss.Width(paragraph) <= width {
			out = append(out, paragraph)
			continue
		}
		words := strings.Fields(paragraph)
		var line strings.Builder
		for _, w := range words {
			if line.Len() == 0 {
				line.WriteString(w)
				continue
			}
			if lipgloss.Width(line.String())+1+lipgloss.Width(w) > width {
				out = append(out, line.String())
				line.Reset()
				line.WriteString(w)
				continue
			}
			line.WriteString(" ")
			line.WriteString(w)
		}
		if line.Len() > 0 {
			out = append(out, line.String())
		}
	}
	return strings.Join(out, "\n")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
