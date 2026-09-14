package actionview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type JobsFetchedMsg struct {
	RunID              int64
	Jobs               []data.WorkflowJob
	Err                error
	RepositoryIdentity string
}

var fetchWorkflowJobs = data.FetchWorkflowJobs

type Model struct {
	ctx                *context.ProgramContext
	run                *data.ActionRun
	jobs               []data.WorkflowJob
	loading            bool
	err                error
	width              int
	repositoryIdentity string
}

func NewModel(ctx *context.ProgramContext) Model { return Model{ctx: ctx} }

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) { m.ctx = ctx }
func (m *Model) SetWidth(width int)                               { m.width = width }

func (m *Model) SetRun(run *data.ActionRun) tea.Cmd {
	if run == nil {
		m.Reset()
		return nil
	}
	repositoryIdentity := m.ctx.ActionsRepositoryIdentity()
	if m.run != nil && m.run.ID == run.ID && m.repositoryIdentity == repositoryIdentity {
		return nil
	}
	copy := *run
	m.run = &copy
	m.jobs = nil
	m.err = nil
	m.loading = true
	m.repositoryIdentity = repositoryIdentity
	repo := m.ctx.ActionsRepository()
	if repo == nil {
		m.loading = false
		return nil
	}
	repoCopy := *repo
	return func() tea.Msg {
		jobs, err := fetchWorkflowJobs(repoCopy.Host, repoCopy.Owner, repoCopy.Name, run.ID)
		return JobsFetchedMsg{RunID: run.ID, Jobs: jobs, Err: err, RepositoryIdentity: repositoryIdentity}
	}
}

func (m *Model) SetJobs(msg JobsFetchedMsg) {
	if m.run == nil || m.run.ID != msg.RunID || msg.RepositoryIdentity != m.repositoryIdentity ||
		msg.RepositoryIdentity != m.ctx.ActionsRepositoryIdentity() {
		return
	}
	m.loading = false
	m.jobs = msg.Jobs
	m.err = msg.Err
}

func (m *Model) Reset() {
	m.run = nil
	m.jobs = nil
	m.loading = false
	m.err = nil
	m.repositoryIdentity = ""
}

func (m Model) View() string {
	if m.run == nil {
		return ""
	}
	run := m.run
	status := run.Status
	if run.Conclusion != "" {
		status += " / " + run.Conclusion
	}
	rows := []string{
		lipgloss.NewStyle().Bold(true).Foreground(m.ctx.Theme.PrimaryText).Render(run.DisplayTitle),
		"",
		fmt.Sprintf("Workflow   %s", run.Name),
		fmt.Sprintf("Status     %s", status),
		fmt.Sprintf("Branch     %s", run.HeadBranch),
		fmt.Sprintf("Event      %s", run.Event),
		fmt.Sprintf("Actor      %s", run.Actor.Login),
		fmt.Sprintf("Run        #%d (%d)", run.RunNumber, run.ID),
		fmt.Sprintf("Commit     %s", shortSHA(run.HeadSHA)),
		fmt.Sprintf("Created    %s (%s)", run.CreatedAt.Format(time.RFC822), utils.TimeElapsed(run.CreatedAt)),
		fmt.Sprintf("Updated    %s", run.UpdatedAt.Format(time.RFC822)),
		"",
		lipgloss.NewStyle().Bold(true).Render("Jobs"),
	}
	if m.loading {
		rows = append(rows, "Loading jobs...")
	} else if m.err != nil {
		rows = append(rows, "Unable to load jobs: "+m.err.Error())
	} else if len(m.jobs) == 0 {
		rows = append(rows, "No jobs")
	} else {
		for _, job := range m.jobs {
			state := job.Status
			if job.Conclusion != "" {
				state += " / " + job.Conclusion
			}
			rows = append(rows, fmt.Sprintf("• %s — %s", job.Name, state))
		}
	}
	return lipgloss.NewStyle().Width(max(m.width, 1)).Render(strings.Join(rows, "\n"))
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
