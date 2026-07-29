package actionsrow

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type Row struct {
	Ctx *context.ProgramContext
	Run data.ActionRun
}

func (r Row) ToTableRow() table.Row {
	status, color := statusDisplay(r.Run)
	style := lipgloss.NewStyle().Foreground(color)
	text := lipgloss.NewStyle().Foreground(r.Ctx.Theme.PrimaryText)
	return table.Row{
		style.Render(status),
		text.Render(r.Run.Name),
		text.Render(r.Run.DisplayTitle),
		text.Render(r.Run.HeadBranch),
		text.Render(r.Run.Event),
		text.Render(utils.TimeElapsed(r.Run.UpdatedAt)),
	}
}

func statusDisplay(run data.ActionRun) (string, color.Color) {
	value := run.Status
	if run.Status == "completed" && run.Conclusion != "" {
		value = run.Conclusion
	}
	color := lipgloss.Color("#e0af68")
	switch strings.ToLower(value) {
	case "success", "neutral", "skipped":
		color = lipgloss.Color("#9ece6a")
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure":
		color = lipgloss.Color("#f7768e")
	case "in_progress", "queued", "waiting", "pending", "requested":
		color = lipgloss.Color("#7aa2f7")
	}
	return value, color
}
