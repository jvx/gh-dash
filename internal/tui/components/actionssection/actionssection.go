package actionssection

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/actionsrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

const SectionType = "action"

type Model struct {
	section.BaseModel
	Runs []data.ActionRun
	page int
}

type RunsFetchedMsg struct {
	Runs       []data.ActionRun
	TotalCount int
	HasNext    bool
	Page       int
	TaskID     string
}

type RunsFetchFailedMsg struct {
	TaskID string
}

func NewModel(id int, ctx *context.ProgramContext, lastUpdated time.Time) Model {
	view := config.ActionsView
	cfg := config.SectionConfig{Title: "Workflow Runs", Type: &view}
	m := Model{}
	m.BaseModel = section.NewModel(ctx, section.NewSectionOptions{
		Id: id, Config: cfg, Type: SectionType, Columns: columns(), Singular: "run", Plural: "runs",
		LastUpdated: lastUpdated, CreatedAt: lastUpdated,
	})
	m.IsSearchSupported = false
	m.Runs = []data.ActionRun{}
	return m
}

func columns() []table.Column {
	return []table.Column{
		{Title: "Status", Width: utils.IntPtr(14)},
		{Title: "Workflow", Width: utils.IntPtr(20)},
		{Title: "Title", Grow: utils.BoolPtr(true)},
		{Title: "Branch", Width: utils.IntPtr(18)},
		{Title: "Event", Width: utils.IntPtr(14)},
		{Title: "Age", Width: utils.IntPtr(8)},
	}
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.IsPromptConfirmationFocused() {
			switch msg.String() {
			case "ctrl+c", "esc":
				m.PromptConfirmationBox.Reset()
				return m, m.SetIsPromptConfirmationShown(false)
			case "enter":
				confirmed := m.PromptConfirmationBox.Value()
				if confirmed == "y" || confirmed == "Y" {
					cmd = m.Rerun()
				}
				m.PromptConfirmationBox.Reset()
				return m, tea.Batch(cmd, m.SetIsPromptConfirmationShown(false))
			}
		}
	case RunsFetchedMsg:
		if msg.TaskID == m.LastFetchTaskId {
			if msg.Page > 1 {
				m.Runs = append(m.Runs, msg.Runs...)
			} else {
				m.Runs = msg.Runs
			}
			m.page = msg.Page
			m.TotalCount = msg.TotalCount
			m.PageInfo = &data.PageInfo{HasNextPage: msg.HasNext, EndCursor: strconv.Itoa(msg.Page + 1)}
			m.SetIsLoading(false)
			m.Table.SetRows(m.BuildRows())
			m.Table.UpdateLastUpdated(time.Now())
			m.UpdateTotalItemsCount(msg.TotalCount)
		}
	case RunsFetchFailedMsg:
		if msg.TaskID == m.LastFetchTaskId {
			m.SetIsLoading(false)
		}
	case processFinishedMsg:
		m.ResetRows()
		return m, tea.Batch(m.FetchNextPageSectionRows()...)
	}

	prompt, promptCmd := m.PromptConfirmationBox.Update(msg)
	m.PromptConfirmationBox = prompt
	tbl, tableCmd := m.Table.Update(msg)
	m.Table = tbl
	return m, tea.Batch(cmd, promptCmd, tableCmd)
}

func (m *Model) BuildRows() []table.Row {
	rows := make([]table.Row, 0, len(m.Runs))
	for _, run := range m.Runs {
		rows = append(rows, actionsrow.Row{Ctx: m.Ctx, Run: run}.ToTableRow())
	}
	return rows
}

func (m *Model) NumRows() int { return len(m.Runs) }

func (m *Model) GetCurrRow() data.RowData {
	idx := m.Table.GetCurrItem()
	if idx < 0 || idx >= len(m.Runs) {
		return nil
	}
	return &m.Runs[idx]
}

func (m *Model) CurrentRun() *data.ActionRun {
	idx := m.Table.GetCurrItem()
	if idx < 0 || idx >= len(m.Runs) {
		return nil
	}
	return &m.Runs[idx]
}

func (m *Model) FetchNextPageSectionRows() []tea.Cmd {
	if m == nil || !m.Ctx.HasGHRepo() || (m.PageInfo != nil && !m.PageInfo.HasNextPage) {
		return nil
	}
	page := 1
	if m.PageInfo != nil {
		page, _ = strconv.Atoi(m.PageInfo.EndCursor)
	}
	taskID := fmt.Sprintf("fetching_action_runs_%d_%d_%d", m.Id, page, time.Now().UnixNano())
	m.LastFetchTaskId = taskID
	m.SetIsLoading(true)
	start := m.Ctx.StartTask(context.Task{Id: taskID, StartText: "Fetching workflow runs", FinishedText: "Workflow runs fetched", State: context.TaskStart})
	fetch := func() tea.Msg {
		limit := m.Ctx.Config.Defaults.ActionsLimit
		if limit <= 0 {
			limit = 25
		}
		repo := m.Ctx.GHRepo
		result, err := data.FetchActionRuns(repo.Host, repo.Owner, repo.Name, limit, page)
		if err != nil {
			return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: RunsFetchFailedMsg{TaskID: taskID}, Err: err}
		}
		return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: RunsFetchedMsg{
			Runs: result.Runs, TotalCount: result.TotalCount, HasNext: result.HasNextPage, Page: page, TaskID: taskID,
		}}
	}
	return []tea.Cmd{start, fetch, m.Table.StartLoadingSpinner()}
}

func Fetch(ctx *context.ProgramContext) (Model, tea.Cmd) {
	m := NewModel(0, ctx, time.Now())
	if !ctx.HasGHRepo() {
		m.Table.SetRows([]table.Row{})
		return m, func() tea.Msg {
			return constants.ErrMsg{Err: errors.New("Actions view requires a repository; start gh-dash in a repository or set GH_REPO")}
		}
	}
	return m, tea.Batch(m.FetchNextPageSectionRows()...)
}

func (m *Model) ResetRows() {
	m.Runs = nil
	m.page = 0
	m.BaseModel.ResetRows()
}

func (m *Model) GetItemSingularForm() string { return "Run" }
func (m *Model) GetItemPluralForm() string   { return "Runs" }
func (m *Model) GetTotalCount() int          { return m.TotalCount }
func (m *Model) SetIsLoading(value bool) {
	m.IsLoading = value
	m.Table.SetIsLoading(value)
}

func (m *Model) View() string {
	return m.Ctx.Styles.Section.ContainerStyle.Width(m.Ctx.MainContentWidth).Render(m.Table.View())
}

func (m *Model) GetPagerContent() string {
	if m.TotalCount == 0 {
		return ""
	}
	return m.Ctx.Styles.ListViewPort.PagerStyle.Render(fmt.Sprintf("Run %d/%d • Fetched %d", m.Table.GetCurrItem()+1, m.TotalCount, len(m.Runs)))
}

func (m *Model) GetPromptConfirmation() string {
	if !m.IsPromptConfirmationShown {
		return ""
	}
	m.PromptConfirmationBox.SetPrompt("Are you sure you want to rerun this workflow run? (y/N) ")
	return m.Ctx.Styles.ListViewPort.PagerStyle.Render(m.PromptConfirmationBox.View())
}

func repoArg(ctx *context.ProgramContext) string {
	repo := ctx.GHRepo
	if repo.Host != "" && repo.Host != "github.com" {
		return fmt.Sprintf("%s/%s/%s", repo.Host, repo.Owner, repo.Name)
	}
	return fmt.Sprintf("%s/%s", repo.Owner, repo.Name)
}

func (m *Model) Watch() tea.Cmd {
	run := m.CurrentRun()
	if run == nil {
		return nil
	}
	return execCommand(watchArgs(m.Ctx, run.ID))
}

func (m *Model) Dispatch() tea.Cmd {
	if !m.Ctx.HasGHRepo() {
		return nil
	}
	return execCommand(dispatchArgs(m.Ctx))
}

func (m *Model) Rerun() tea.Cmd {
	run := m.CurrentRun()
	if run == nil {
		return nil
	}
	return execCommand(rerunArgs(m.Ctx, run.ID))
}

func watchArgs(ctx *context.ProgramContext, runID int64) []string {
	return []string{"run", "watch", strconv.FormatInt(runID, 10), "--repo", repoArg(ctx)}
}

func dispatchArgs(ctx *context.ProgramContext) []string {
	return []string{"workflow", "run", "--repo", repoArg(ctx)}
}

func rerunArgs(ctx *context.ProgramContext, runID int64) []string {
	return []string{"run", "rerun", strconv.FormatInt(runID, 10), "--repo", repoArg(ctx)}
}

func execCommand(args []string) tea.Cmd {
	cmd := exec.Command("gh", args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return constants.ErrMsg{Err: err}
		}
		return processFinishedMsg{}
	})
}

type processFinishedMsg struct{}
