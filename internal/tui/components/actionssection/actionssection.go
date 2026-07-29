package actionssection

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/actionsrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

const SectionType = "action"

const defaultWorkflowPaneWidth = 28

type Model struct {
	section.BaseModel
	Runs []data.ActionRun
	page int

	Workflows               []data.Workflow
	workflowCursor          int
	selectedWorkflowID      int64
	workflowListFocused     bool
	workflowsLoading        bool
	workflowLoadFailed      bool
	lastWorkflowFetchTaskID string
	workflowRefreshNeeded   bool
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

type WorkflowsFetchedMsg struct {
	Workflows []data.Workflow
	TaskID    string
}

type WorkflowsFetchFailedMsg struct {
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
	m.Workflows = []data.Workflow{}
	m.workflowListFocused = true
	m.workflowRefreshNeeded = true
	m.syncTableDimensions()
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
					switch m.GetPromptConfirmationAction() {
					case "dispatch":
						cmd = m.Dispatch()
					default:
						cmd = m.Rerun()
					}
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
	case WorkflowsFetchedMsg:
		if msg.TaskID == m.lastWorkflowFetchTaskID {
			m.Workflows = msg.Workflows
			sort.SliceStable(m.Workflows, func(i, j int) bool {
				return strings.ToLower(m.Workflows[i].Name) < strings.ToLower(m.Workflows[j].Name)
			})
			m.workflowsLoading = false
			m.workflowLoadFailed = false
			m.clampWorkflowCursor()
			if m.selectedWorkflowID != 0 && m.SelectedWorkflow() == nil {
				m.selectedWorkflowID = 0
				m.resetRuns()
				cmd = tea.Batch(m.FetchNextPageSectionRows()...)
			}
		}
	case WorkflowsFetchFailedMsg:
		if msg.TaskID == m.lastWorkflowFetchTaskID {
			m.workflowsLoading = false
			m.workflowLoadFailed = true
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
	workflowID := m.selectedWorkflowID
	start := m.Ctx.StartTask(context.Task{Id: taskID, StartText: "Fetching workflow runs", FinishedText: "Workflow runs fetched", State: context.TaskStart})
	fetch := func() tea.Msg {
		limit := m.Ctx.Config.Defaults.ActionsLimit
		if limit <= 0 {
			limit = 25
		}
		repo := m.Ctx.GHRepo
		result, err := data.FetchActionRuns(repo.Host, repo.Owner, repo.Name, workflowID, limit, page)
		if err != nil {
			return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: RunsFetchFailedMsg{TaskID: taskID}, Err: err}
		}
		return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: RunsFetchedMsg{
			Runs: result.Runs, TotalCount: result.TotalCount, HasNext: result.HasNextPage, Page: page, TaskID: taskID,
		}}
	}
	cmds := []tea.Cmd{start, fetch, m.Table.StartLoadingSpinner()}
	if page == 1 && m.workflowRefreshNeeded {
		m.workflowRefreshNeeded = false
		cmds = append(cmds, m.fetchWorkflowList()...)
	}
	return cmds
}

func (m *Model) fetchWorkflowList() []tea.Cmd {
	if m == nil || !m.Ctx.HasGHRepo() {
		return nil
	}
	taskID := fmt.Sprintf("fetching_workflows_%d_%d", m.Id, time.Now().UnixNano())
	m.lastWorkflowFetchTaskID = taskID
	m.workflowsLoading = true
	m.workflowLoadFailed = false
	start := m.Ctx.StartTask(context.Task{Id: taskID, StartText: "Fetching workflows", FinishedText: "Workflows fetched", State: context.TaskStart})
	fetch := func() tea.Msg {
		repo := m.Ctx.GHRepo
		workflows, err := data.FetchWorkflows(repo.Host, repo.Owner, repo.Name)
		if err != nil {
			return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: WorkflowsFetchFailedMsg{TaskID: taskID}, Err: err}
		}
		return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: WorkflowsFetchedMsg{Workflows: workflows, TaskID: taskID}}
	}
	return []tea.Cmd{start, fetch}
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

func (m *Model) resetRuns() {
	m.Runs = nil
	m.page = 0
	m.TotalCount = 0
	m.PageInfo = nil
	m.Table.SetRows(nil)
	m.Table.ResetCurrItem()
	m.UpdateTotalItemsCount(0)
}

func (m *Model) ResetRows() {
	m.resetRuns()
	m.Workflows = nil
	m.workflowRefreshNeeded = true
	m.workflowLoadFailed = false
}

func (m *Model) GetItemSingularForm() string { return "Run" }
func (m *Model) GetItemPluralForm() string   { return "Runs" }
func (m *Model) GetTotalCount() int          { return m.TotalCount }
func (m *Model) SetIsLoading(value bool) {
	m.IsLoading = value
	m.Table.SetIsLoading(value)
}

func (m *Model) workflowPaneWidth() int {
	available := m.BaseModel.GetDimensions().Width
	if available <= 1 {
		return 0
	}
	width := defaultWorkflowPaneWidth
	if available < 90 {
		width = max(18, min(24, available/3))
	}
	if available < 45 {
		width = max(10, available/2)
	}
	return min(width, available-1)
}

func (m *Model) syncTableDimensions() {
	dimensions := m.BaseModel.GetDimensions()
	paneWidth := m.workflowPaneWidth()
	m.Table.SetDimensions(constants.Dimensions{
		Width:  max(0, dimensions.Width-paneWidth-1),
		Height: max(0, dimensions.Height-2),
	})
	m.Table.SyncViewPortContent()
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.BaseModel.UpdateProgramContext(ctx)
	m.syncTableDimensions()
}

func (m *Model) View() string {
	tableView := m.Table.View()
	paneWidth := m.workflowPaneWidth()
	if paneWidth == 0 {
		return m.Ctx.Styles.Section.ContainerStyle.Width(m.Ctx.MainContentWidth).Render(tableView)
	}
	pane := m.renderWorkflowPane(paneWidth, lipgloss.Height(tableView))
	content := lipgloss.JoinHorizontal(lipgloss.Top, pane, tableView)
	return m.Ctx.Styles.Section.ContainerStyle.Width(m.Ctx.MainContentWidth).Render(content)
}

func (m *Model) renderWorkflowPane(width, height int) string {
	header := m.Ctx.Styles.Table.HeaderStyle.
		Width(width).
		Height(common.TableHeaderHeight).
		Bold(true).
		Render("Workflows")
	bodyHeight := max(0, height-common.TableHeaderHeight)
	lines := m.workflowLines(width, bodyHeight)
	body := lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(m.Ctx.Theme.FaintBorder).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m *Model) workflowLines(width, visible int) []string {
	if visible <= 0 {
		return nil
	}
	if m.workflowsLoading && len(m.Workflows) == 0 {
		return []string{m.Ctx.Styles.Common.FaintTextStyle.Width(width).Render(" Loading...")}
	}
	if m.workflowLoadFailed && len(m.Workflows) == 0 {
		return []string{m.Ctx.Styles.Common.FaintTextStyle.Width(width).Render(" Failed to load")}
	}

	count := m.WorkflowItemCount()
	start := 0
	if m.workflowCursor >= visible {
		start = m.workflowCursor - visible + 1
	}
	if start+visible > count {
		start = max(0, count-visible)
	}
	end := min(count, start+visible)
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		name, state, id := m.workflowItem(index)
		if state != "" && state != "active" {
			name += " (disabled)"
		}
		name = ansi.Truncate(name, max(1, width-4), constants.Ellipsis)
		cursor := " "
		if m.workflowListFocused && index == m.workflowCursor {
			cursor = ">"
		}
		selected := " "
		if id == m.selectedWorkflowID {
			selected = "*"
		}
		line := fmt.Sprintf("%s%s %s", cursor, selected, name)
		style := lipgloss.NewStyle().Width(width).MaxWidth(width)
		if state != "" && state != "active" {
			style = style.Foreground(m.Ctx.Theme.FaintText)
		}
		if m.workflowListFocused && index == m.workflowCursor {
			style = style.Foreground(m.Ctx.Theme.PrimaryText).Background(m.Ctx.Theme.SelectedBackground).Bold(true)
		}
		lines = append(lines, style.Render(line))
	}
	return lines
}

func (m *Model) workflowItem(index int) (name, state string, id int64) {
	if index <= 0 {
		return "All workflows", "active", 0
	}
	if index > len(m.Workflows) {
		return "", "", 0
	}
	workflow := m.Workflows[index-1]
	return workflow.Name, workflow.State, workflow.ID
}

func (m *Model) WorkflowItemCount() int { return len(m.Workflows) + 1 }
func (m *Model) WorkflowCursor() int    { return m.workflowCursor }
func (m *Model) WorkflowsFocused() bool { return m.workflowListFocused }
func (m *Model) FocusWorkflows()        { m.workflowListFocused = true }
func (m *Model) FocusRuns()             { m.workflowListFocused = false }

func (m *Model) MoveWorkflowCursor(delta int) {
	m.workflowCursor += delta
	m.clampWorkflowCursor()
}

func (m *Model) FirstWorkflow() { m.workflowCursor = 0 }
func (m *Model) LastWorkflow()  { m.workflowCursor = max(0, m.WorkflowItemCount()-1) }

func (m *Model) clampWorkflowCursor() {
	m.workflowCursor = max(0, min(m.workflowCursor, m.WorkflowItemCount()-1))
}

func (m *Model) SelectWorkflow() tea.Cmd {
	_, _, workflowID := m.workflowItem(m.workflowCursor)
	if workflowID == m.selectedWorkflowID {
		return nil
	}
	m.selectedWorkflowID = workflowID
	m.resetRuns()
	return tea.Batch(m.FetchNextPageSectionRows()...)
}

func (m *Model) SelectedWorkflow() *data.Workflow {
	if m.selectedWorkflowID == 0 {
		return nil
	}
	for i := range m.Workflows {
		if m.Workflows[i].ID == m.selectedWorkflowID {
			return &m.Workflows[i]
		}
	}
	return nil
}

func (m *Model) SelectedWorkflowID() int64 { return m.selectedWorkflowID }

func (m *Model) ValidateDispatch() error {
	workflow := m.SelectedWorkflow()
	if workflow == nil {
		return errors.New("select a workflow from the left sidebar before running it")
	}
	if workflow.State != "active" {
		return fmt.Errorf("workflow %q is disabled", workflow.Name)
	}
	return nil
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
	promptText := "Are you sure you want to rerun this workflow run? (y/N) "
	if m.GetPromptConfirmationAction() == "dispatch" {
		if workflow := m.SelectedWorkflow(); workflow != nil {
			promptText = fmt.Sprintf("Run workflow %q? (y/N) ", workflow.Name)
		}
	}
	m.PromptConfirmationBox.SetPrompt(promptText)
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
	workflow := m.SelectedWorkflow()
	if workflow == nil || workflow.State != "active" {
		return nil
	}
	return execCommand(dispatchArgs(m.Ctx, workflow.ID))
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

func dispatchArgs(ctx *context.ProgramContext, workflowID int64) []string {
	return []string{"workflow", "run", strconv.FormatInt(workflowID, 10), "--repo", repoArg(ctx)}
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
