package actionssection

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/sahilm/fuzzy"

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

const runsPollInterval = 5 * time.Second

var (
	fetchActionRuns          = data.FetchActionRuns
	fetchActionWorkflows     = data.FetchWorkflows
	fetchActionsRepositories = data.FetchActionRepositories
)

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
	pollScheduled           bool
	pollToken               int64

	repositoryPickerOpen  bool
	repositoriesLoading   bool
	repositoriesLoadError error
	repositories          []data.ActionRepository
	filteredRepositories  []data.ActionRepository
	repositoryQuery       string
	repositoryCursor      int
	repositoryRequestID   int64
}

type RunsFetchedMsg struct {
	Runs               []data.ActionRun
	TotalCount         int
	HasNext            bool
	Page               int
	TaskID             string
	RepositoryIdentity string
}

type RunsFetchFailedMsg struct {
	TaskID             string
	RepositoryIdentity string
}

type WorkflowsFetchedMsg struct {
	Workflows          []data.Workflow
	TaskID             string
	RepositoryIdentity string
}

type WorkflowsFetchFailedMsg struct {
	TaskID             string
	RepositoryIdentity string
}

type RepositoriesFetchedMsg struct {
	Repositories []data.ActionRepository
	Err          error
	RequestID    int64
}

// RunsPollTickMsg and RunsPolledMsg carry their section ID so the root model can
// route polling updates even when the user has switched to another view.
type RunsPollTickMsg struct {
	SectionID          int
	Token              int64
	RepositoryIdentity string
}

func (m RunsPollTickMsg) ActionSectionID() int { return m.SectionID }

type RunsPolledMsg struct {
	SectionID          int
	Token              int64
	Result             data.ActionRunsResponse
	Err                error
	RepositoryIdentity string
}

func (m RunsPolledMsg) ActionSectionID() int { return m.SectionID }

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
		if m.repositoryPickerOpen {
			return m, m.updateRepositoryPicker(msg)
		}
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
		if msg.TaskID == m.LastFetchTaskId && m.matchesRepository(msg.RepositoryIdentity) {
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
			if msg.Page == 1 {
				cmd = m.scheduleRunsPollIfNeeded()
			}
		}
	case RunsFetchFailedMsg:
		if msg.TaskID == m.LastFetchTaskId && m.matchesRepository(msg.RepositoryIdentity) {
			m.SetIsLoading(false)
		}
	case WorkflowsFetchedMsg:
		if msg.TaskID == m.lastWorkflowFetchTaskID && m.matchesRepository(msg.RepositoryIdentity) {
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
		if msg.TaskID == m.lastWorkflowFetchTaskID && m.matchesRepository(msg.RepositoryIdentity) {
			m.workflowsLoading = false
			m.workflowLoadFailed = true
		}
	case RunsPollTickMsg:
		if msg.Token == m.pollToken && m.pollScheduled && m.matchesRepository(msg.RepositoryIdentity) {
			m.pollScheduled = false
			if m.hasInProgressRuns() {
				cmd = m.pollRuns(msg.Token)
			}
		}
	case RunsPolledMsg:
		if msg.Token == m.pollToken && m.matchesRepository(msg.RepositoryIdentity) {
			m.pollScheduled = false
			if msg.Err == nil {
				m.Runs = msg.Result.Runs
				m.page = 1
				m.TotalCount = msg.Result.TotalCount
				m.PageInfo = &data.PageInfo{HasNextPage: msg.Result.HasNextPage, EndCursor: "2"}
				m.Table.SetRows(m.BuildRows())
				m.Table.UpdateLastUpdated(time.Now())
				m.UpdateTotalItemsCount(msg.Result.TotalCount)
			}
			cmd = m.scheduleRunsPollIfNeeded()
		}
	case RepositoriesFetchedMsg:
		if m.repositoryPickerOpen && msg.RequestID == m.repositoryRequestID {
			m.repositoriesLoading = false
			m.repositoriesLoadError = msg.Err
			if msg.Err == nil {
				m.repositories = msg.Repositories
				m.filterRepositories()
			}
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
	if m == nil || !m.Ctx.HasActionsRepository() || (m.PageInfo != nil && !m.PageInfo.HasNextPage) {
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
	repo := *m.Ctx.ActionsRepository()
	repositoryIdentity := context.RepositoryIdentity(&repo)
	start := m.Ctx.StartTask(context.Task{Id: taskID, StartText: "Fetching workflow runs", FinishedText: "Workflow runs fetched", State: context.TaskStart})
	fetch := func() tea.Msg {
		limit := m.Ctx.Config.Defaults.ActionsLimit
		if limit <= 0 {
			limit = 25
		}
		result, err := fetchActionRuns(repo.Host, repo.Owner, repo.Name, workflowID, limit, page)
		if err != nil {
			return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: RunsFetchFailedMsg{TaskID: taskID, RepositoryIdentity: repositoryIdentity}, Err: err}
		}
		return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: RunsFetchedMsg{
			Runs: result.Runs, TotalCount: result.TotalCount, HasNext: result.HasNextPage, Page: page, TaskID: taskID,
			RepositoryIdentity: repositoryIdentity,
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
	if m == nil || !m.Ctx.HasActionsRepository() {
		return nil
	}
	taskID := fmt.Sprintf("fetching_workflows_%d_%d", m.Id, time.Now().UnixNano())
	m.lastWorkflowFetchTaskID = taskID
	m.workflowsLoading = true
	m.workflowLoadFailed = false
	repo := *m.Ctx.ActionsRepository()
	repositoryIdentity := context.RepositoryIdentity(&repo)
	start := m.Ctx.StartTask(context.Task{Id: taskID, StartText: "Fetching workflows", FinishedText: "Workflows fetched", State: context.TaskStart})
	fetch := func() tea.Msg {
		workflows, err := fetchActionWorkflows(repo.Host, repo.Owner, repo.Name)
		if err != nil {
			return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: WorkflowsFetchFailedMsg{TaskID: taskID, RepositoryIdentity: repositoryIdentity}, Err: err}
		}
		return constants.TaskFinishedMsg{SectionId: m.Id, SectionType: m.Type, TaskId: taskID, Msg: WorkflowsFetchedMsg{Workflows: workflows, TaskID: taskID, RepositoryIdentity: repositoryIdentity}}
	}
	return []tea.Cmd{start, fetch}
}

func Fetch(ctx *context.ProgramContext) (Model, tea.Cmd) {
	m := NewModel(0, ctx, time.Now())
	if !ctx.HasActionsRepository() {
		return m, m.OpenRepositoryPicker()
	}
	return m, tea.Batch(m.FetchNextPageSectionRows()...)
}

func (m *Model) matchesRepository(identity string) bool {
	return identity == m.Ctx.ActionsRepositoryIdentity()
}

func (m *Model) RepositoryPickerOpen() bool { return m.repositoryPickerOpen }

func (m *Model) repositoryPickerHosts() []string {
	hosts := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(host string) {
		host = auth.NormalizeHostname(host)
		if host == "" {
			return
		}
		identity := strings.ToLower(host)
		if _, ok := seen[identity]; ok {
			return
		}
		seen[identity] = struct{}{}
		hosts = append(hosts, host)
	}
	if repo := m.Ctx.ActionsRepository(); repo != nil {
		add(repo.Host)
	}
	for _, host := range auth.KnownHosts() {
		add(host)
	}
	defaultHost, _ := auth.DefaultHost()
	add(defaultHost)
	if len(hosts) == 0 {
		add("github.com")
	}
	return hosts
}

func (m *Model) OpenRepositoryPicker() tea.Cmd {
	m.repositoryPickerOpen = true
	m.repositoriesLoading = true
	m.repositoriesLoadError = nil
	m.repositoryQuery = ""
	m.repositoryCursor = 0
	m.repositories = nil
	m.filteredRepositories = nil
	m.repositoryRequestID++
	requestID := m.repositoryRequestID
	hosts := m.repositoryPickerHosts()
	return func() tea.Msg {
		var repositories []data.ActionRepository
		var lastErr error
		succeeded := false
		for _, host := range hosts {
			hostRepositories, err := fetchActionsRepositories(host)
			if err != nil {
				lastErr = err
				continue
			}
			succeeded = true
			repositories = append(repositories, hostRepositories...)
		}
		if succeeded {
			lastErr = nil
			sort.SliceStable(repositories, func(i, j int) bool {
				return repositories[i].PushedAt.After(repositories[j].PushedAt)
			})
		}
		return RepositoriesFetchedMsg{Repositories: repositories, Err: lastErr, RequestID: requestID}
	}
}

func (m *Model) filterRepositories() {
	m.filteredRepositories = append(m.filteredRepositories[:0], m.repositories...)
	query := strings.TrimSpace(m.repositoryQuery)
	if query != "" {
		searchable := make([]string, len(m.repositories))
		for i, repo := range m.repositories {
			searchable[i] = repo.Host + "/" + repo.FullName
		}
		matches := fuzzy.Find(query, searchable)
		m.filteredRepositories = m.filteredRepositories[:0]
		for _, match := range matches {
			m.filteredRepositories = append(m.filteredRepositories, m.repositories[match.Index])
		}
	}
	m.repositoryCursor = max(0, min(m.repositoryCursor, len(m.filteredRepositories)-1))
}

func (m *Model) updateRepositoryPicker(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.repositoryPickerOpen = false
		m.repositoryRequestID++
		return nil
	case "up", "ctrl+p":
		if len(m.filteredRepositories) > 0 {
			m.repositoryCursor = (m.repositoryCursor - 1 + len(m.filteredRepositories)) % len(m.filteredRepositories)
		}
		return nil
	case "down", "ctrl+n":
		if len(m.filteredRepositories) > 0 {
			m.repositoryCursor = (m.repositoryCursor + 1) % len(m.filteredRepositories)
		}
		return nil
	case "enter":
		if m.repositoryCursor < 0 || m.repositoryCursor >= len(m.filteredRepositories) {
			return nil
		}
		selected := m.filteredRepositories[m.repositoryCursor]
		oldIdentity := m.Ctx.ActionsRepositoryIdentity()
		m.Ctx.SetActionsRepository(repository.Repository{Host: selected.Host, Owner: selected.Owner, Name: selected.Name})
		m.repositoryPickerOpen = false
		if oldIdentity == m.Ctx.ActionsRepositoryIdentity() {
			return nil
		}
		m.resetForRepositoryChange()
		return tea.Batch(m.FetchNextPageSectionRows()...)
	case "backspace":
		if m.repositoryQuery != "" {
			_, size := utf8.DecodeLastRuneInString(m.repositoryQuery)
			m.repositoryQuery = m.repositoryQuery[:len(m.repositoryQuery)-size]
			m.repositoryCursor = 0
			m.filterRepositories()
		}
		return nil
	}
	text := msg.Key().Text
	if text != "" && utf8.RuneCountInString(text) > 0 {
		m.repositoryQuery += text
		m.repositoryCursor = 0
		m.filterRepositories()
	}
	return nil
}

func (m *Model) resetForRepositoryChange() {
	m.cancelRunsPoll()
	m.LastFetchTaskId = ""
	m.lastWorkflowFetchTaskID = ""
	m.Runs = nil
	m.Workflows = nil
	m.page = 0
	m.TotalCount = 0
	m.PageInfo = nil
	m.selectedWorkflowID = 0
	m.workflowCursor = 0
	m.workflowListFocused = true
	m.workflowsLoading = false
	m.workflowLoadFailed = false
	m.workflowRefreshNeeded = true
	m.Table.SetRows(nil)
	m.Table.ResetCurrItem()
	m.SetIsLoading(false)
	m.UpdateTotalItemsCount(0)
}

func (m *Model) renderRepositoryPicker() string {
	width := max(1, min(72, m.Ctx.MainContentWidth-2))
	height := max(1, m.Ctx.MainContentHeight-3)
	var lines []string
	lines = append(lines, m.Ctx.Styles.Table.HeaderStyle.Bold(true).Render("Choose Actions repository"))
	lines = append(lines, ansi.Truncate("Search: "+m.repositoryQuery+"█", width, constants.Ellipsis))
	lines = append(lines, m.Ctx.Styles.Common.FaintTextStyle.Render("↑/↓ or ctrl+p/n • enter select • esc cancel"))
	if m.repositoriesLoading {
		lines = append(lines, "Loading repositories...")
	} else if m.repositoriesLoadError != nil {
		lines = append(lines, "Unable to load repositories: "+m.repositoriesLoadError.Error())
	} else if len(m.filteredRepositories) == 0 {
		lines = append(lines, "No repositories found")
	} else {
		visible := max(1, height-len(lines))
		start := max(0, m.repositoryCursor-visible+1)
		end := min(len(m.filteredRepositories), start+visible)
		for i := start; i < end; i++ {
			repo := m.filteredRepositories[i]
			label := repo.FullName
			if repo.Host != "github.com" {
				label = repo.Host + "/" + label
			}
			prefix := "  "
			style := lipgloss.NewStyle()
			if i == m.repositoryCursor {
				prefix = "> "
				style = style.Background(m.Ctx.Theme.SelectedBackground).Bold(true)
			}
			lines = append(lines, style.Width(width).MaxWidth(width).Render(prefix+ansi.Truncate(label, max(1, width-2), constants.Ellipsis)))
		}
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Height(height).MaxHeight(height).Render(strings.Join(lines, "\n"))
}

func (m *Model) resetRuns() {
	m.cancelRunsPoll()
	m.Runs = nil
	m.page = 0
	m.TotalCount = 0
	m.PageInfo = nil
	m.Table.SetRows(nil)
	m.Table.ResetCurrItem()
	m.UpdateTotalItemsCount(0)
}

func (m *Model) hasInProgressRuns() bool {
	for _, run := range m.Runs {
		if run.Status != "completed" {
			return true
		}
	}
	return false
}

func (m *Model) nextPollToken() int64 {
	m.pollToken++
	return m.pollToken
}

func (m *Model) cancelRunsPoll() {
	m.pollScheduled = false
	m.nextPollToken()
}

func (m *Model) scheduleRunsPollIfNeeded() tea.Cmd {
	if m.pollScheduled || !m.hasInProgressRuns() {
		return nil
	}
	m.pollScheduled = true
	token := m.nextPollToken()
	sectionID := m.Id
	repositoryIdentity := m.Ctx.ActionsRepositoryIdentity()
	return tea.Tick(runsPollInterval, func(time.Time) tea.Msg {
		return RunsPollTickMsg{SectionID: sectionID, Token: token, RepositoryIdentity: repositoryIdentity}
	})
}

func (m *Model) pollRuns(token int64) tea.Cmd {
	if m == nil || !m.Ctx.HasActionsRepository() {
		return nil
	}
	workflowID := m.selectedWorkflowID
	sectionID := m.Id
	limit := m.Ctx.Config.Defaults.ActionsLimit
	if limit <= 0 {
		limit = 25
	}
	repo := *m.Ctx.ActionsRepository()
	repositoryIdentity := context.RepositoryIdentity(&repo)
	return func() tea.Msg {
		result, err := fetchActionRuns(repo.Host, repo.Owner, repo.Name, workflowID, limit, 1)
		return RunsPolledMsg{SectionID: sectionID, Token: token, Result: result, Err: err, RepositoryIdentity: repositoryIdentity}
	}
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
		Height: max(0, dimensions.Height-3),
	})
	m.Table.SyncViewPortContent()
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.BaseModel.UpdateProgramContext(ctx)
	m.syncTableDimensions()
}

func (m *Model) View() string {
	if m.repositoryPickerOpen {
		return m.Ctx.Styles.Section.ContainerStyle.Width(max(1, m.Ctx.MainContentWidth)).Render(m.renderRepositoryPicker())
	}
	tableView := m.Table.View()
	paneWidth := m.workflowPaneWidth()
	var content string
	if paneWidth == 0 {
		content = tableView
	} else {
		pane := m.renderWorkflowPane(paneWidth, lipgloss.Height(tableView))
		content = lipgloss.JoinHorizontal(lipgloss.Top, pane, tableView)
	}
	repositoryLabel := "No Actions repository selected — press c to choose"
	if repo := m.Ctx.ActionsRepository(); repo != nil {
		repositoryLabel = "Actions repository: " + repo.Owner + "/" + repo.Name
		if repo.Host != "" && repo.Host != "github.com" {
			repositoryLabel = "Actions repository: " + repo.Host + "/" + repo.Owner + "/" + repo.Name
		}
	}
	header := m.Ctx.Styles.Common.FaintTextStyle.Render(ansi.Truncate(repositoryLabel, max(1, m.Ctx.MainContentWidth), constants.Ellipsis))
	return m.Ctx.Styles.Section.ContainerStyle.Width(m.Ctx.MainContentWidth).Render(lipgloss.JoinVertical(lipgloss.Left, header, content))
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
	repo := ctx.ActionsRepository()
	if repo == nil {
		return ""
	}
	if repo.Host != "" && repo.Host != "github.com" {
		return fmt.Sprintf("%s/%s/%s", repo.Host, repo.Owner, repo.Name)
	}
	return fmt.Sprintf("%s/%s", repo.Owner, repo.Name)
}

func (m *Model) Watch() tea.Cmd {
	run := m.CurrentRun()
	if run == nil || !m.Ctx.HasActionsRepository() {
		return nil
	}
	return execCommand(watchArgs(m.Ctx, run.ID))
}

func (m *Model) Dispatch() tea.Cmd {
	workflow := m.SelectedWorkflow()
	if workflow == nil || workflow.State != "active" || !m.Ctx.HasActionsRepository() {
		return nil
	}
	return execCommand(dispatchArgs(m.Ctx, workflow.ID))
}

func (m *Model) Rerun() tea.Cmd {
	run := m.CurrentRun()
	if run == nil || !m.Ctx.HasActionsRepository() {
		return nil
	}
	return execCommand(rerunArgs(m.Ctx, run.ID))
}

func watchArgs(ctx *context.ProgramContext, runID int64) []string {
	if !ctx.HasActionsRepository() {
		return nil
	}
	return []string{"run", "watch", strconv.FormatInt(runID, 10), "--repo", repoArg(ctx)}
}

func dispatchArgs(ctx *context.ProgramContext, workflowID int64) []string {
	if !ctx.HasActionsRepository() {
		return nil
	}
	return []string{"workflow", "run", strconv.FormatInt(workflowID, 10), "--repo", repoArg(ctx)}
}

func rerunArgs(ctx *context.ProgramContext, runID int64) []string {
	if !ctx.HasActionsRepository() {
		return nil
	}
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
