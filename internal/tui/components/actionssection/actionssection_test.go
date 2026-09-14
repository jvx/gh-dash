package actionssection

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func actionsTestContext(host string) *context.ProgramContext {
	cfg := config.Config{
		Defaults: config.Defaults{ActionsLimit: 25},
		Theme:    &config.ThemeConfig{},
	}
	ctx := &context.ProgramContext{
		Config:            &cfg,
		GHRepo:            &repository.Repository{Host: host, Owner: "acme", Name: "widgets"},
		ScreenWidth:       120,
		ScreenHeight:      40,
		MainContentWidth:  120,
		MainContentHeight: 30,
	}
	ctx.Theme = *theme.DefaultTheme
	ctx.Styles = context.InitStyles(ctx.Theme)
	ctx.StartTask = func(context.Task) tea.Cmd { return nil }
	return ctx
}

func TestActionCommandArgsUseInt64IDAndEnterpriseRepo(t *testing.T) {
	ctx := actionsTestContext("ghe.example.com")
	const runID int64 = 9007199254740993
	require.Equal(t,
		[]string{"run", "watch", "9007199254740993", "--repo", "ghe.example.com/acme/widgets"},
		watchArgs(ctx, runID),
	)
	require.Equal(t,
		[]string{"run", "rerun", "9007199254740993", "--repo", "ghe.example.com/acme/widgets"},
		rerunArgs(ctx, runID),
	)
	require.Equal(t,
		[]string{"workflow", "run", "9007199254740993", "--repo", "ghe.example.com/acme/widgets"},
		dispatchArgs(ctx, runID),
	)
}

func TestActionsSectionColumnsAndSearch(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	titles := make([]string, 0, len(model.Columns))
	for _, column := range model.Columns {
		titles = append(titles, column.Title)
	}
	require.Equal(t, []string{"Status", "Workflow", "Title", "Branch", "Event", "Age"}, titles)
	require.False(t, model.IsSearchSupported)

	model.Runs = []data.ActionRun{{ID: 99, RunNumber: 7, Status: "completed", Conclusion: "success"}}
	model.Table.SetRows(model.BuildRows())
	require.Equal(t, int64(99), model.CurrentRun().ID)
	require.Equal(t, 7, model.GetCurrRow().GetNumber())
}

func TestActionsSectionClearsLoadingAfterFetchFailure(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	model.LastFetchTaskId = "latest"
	model.SetIsLoading(true)

	updated, _ := model.Update(RunsFetchFailedMsg{TaskID: "latest", RepositoryIdentity: ctx.ActionsRepositoryIdentity()})
	require.False(t, updated.(*Model).IsLoading)
}

func TestWorkflowPickerSelectsAndFiltersByWorkflow(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	require.True(t, model.WorkflowsFocused())
	require.Equal(t, 0, model.WorkflowCursor())
	require.Equal(t, int64(0), model.SelectedWorkflowID())

	model.lastWorkflowFetchTaskID = "workflows"
	updated, _ := model.Update(WorkflowsFetchedMsg{
		TaskID: "workflows", RepositoryIdentity: ctx.ActionsRepositoryIdentity(),
		Workflows: []data.Workflow{
			{ID: 22, Name: "02 - Production deploy", State: "active"},
			{ID: 11, Name: "01 - Staging deploy", State: "active"},
		},
	})
	model = *updated.(*Model)
	model.MoveWorkflowCursor(1)
	require.Equal(t, 1, model.WorkflowCursor())
	require.Equal(t, "01 - Staging deploy", model.Workflows[0].Name)

	cmd := model.SelectWorkflow()
	require.NotNil(t, cmd)
	require.Equal(t, int64(11), model.SelectedWorkflowID())
	require.True(t, model.IsLoading)
	require.Empty(t, model.Runs)
	require.NotEmpty(t, model.LastFetchTaskId)
}

func TestWorkflowPickerRendersSidebarAndDisabledState(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	model.Workflows = []data.Workflow{
		{ID: 1, Name: "01 - Staging deploy", State: "active"},
		{ID: 2, Name: "Old deploy", State: "disabled_manually"},
	}
	view := model.View()
	require.Contains(t, view, "Workflows")
	require.Contains(t, view, "All workflows")
	require.Contains(t, view, "01 - Staging deploy")
	require.True(t, strings.Contains(view, "Old deploy") || strings.Contains(view, "Old depl"))
	require.Contains(t, view, "Status")
}

func TestDispatchRequiresActiveSelectedWorkflowAndUsesConfirmation(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	require.EqualError(t, model.ValidateDispatch(), "select a workflow from the left sidebar before running it")

	model.Workflows = []data.Workflow{{ID: 77, Name: "Deploy", State: "disabled_manually"}}
	model.workflowCursor = 1
	model.selectedWorkflowID = 77
	require.EqualError(t, model.ValidateDispatch(), `workflow "Deploy" is disabled`)

	model.Workflows[0].State = "active"
	require.NoError(t, model.ValidateDispatch())
	model.SetPromptConfirmationAction("dispatch")
	model.IsPromptConfirmationShown = true
	require.Contains(t, model.GetPromptConfirmation(), `Run workflow "Deploy"?`)
}

func TestWorkflowCursorClampsAndFocusSwitches(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	model.Workflows = []data.Workflow{{ID: 1}, {ID: 2}}

	model.MoveWorkflowCursor(-5)
	require.Equal(t, 0, model.WorkflowCursor())
	model.MoveWorkflowCursor(50)
	require.Equal(t, 2, model.WorkflowCursor())
	model.FirstWorkflow()
	require.Equal(t, 0, model.WorkflowCursor())
	model.LastWorkflow()
	require.Equal(t, 2, model.WorkflowCursor())
	model.FocusRuns()
	require.False(t, model.WorkflowsFocused())
	model.FocusWorkflows()
	require.True(t, model.WorkflowsFocused())
}

func TestWorkflowRefreshFallsBackToAllAndRefetchesRuns(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	model.Workflows = []data.Workflow{{ID: 77, Name: "Removed", State: "active"}}
	model.selectedWorkflowID = 77
	model.Runs = []data.ActionRun{{ID: 99}}
	model.Table.SetRows(model.BuildRows())
	model.lastWorkflowFetchTaskID = "latest"
	oldRunTaskID := model.LastFetchTaskId

	updated, cmd := model.Update(WorkflowsFetchedMsg{TaskID: "latest", RepositoryIdentity: ctx.ActionsRepositoryIdentity(), Workflows: []data.Workflow{{ID: 88, Name: "Current", State: "active"}}})
	model = *updated.(*Model)
	require.NotNil(t, cmd)
	require.Equal(t, int64(0), model.SelectedWorkflowID())
	require.Empty(t, model.Runs)
	require.NotEqual(t, oldRunTaskID, model.LastFetchTaskId)
	require.True(t, model.IsLoading)
}

func TestInProgressRunsPollUntilGitHubReportsCompletion(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(3, ctx, time.Now())
	model.LastFetchTaskId = "initial"

	updated, cmd := model.Update(RunsFetchedMsg{
		TaskID: "initial", RepositoryIdentity: ctx.ActionsRepositoryIdentity(),
		Page: 1,
		Runs: []data.ActionRun{{ID: 99, Status: "queued"}},
	})
	model = *updated.(*Model)
	require.NotNil(t, cmd)
	require.True(t, model.pollScheduled)
	token := model.pollToken

	updated, _ = model.Update(RunsPolledMsg{
		SectionID: 3, RepositoryIdentity: ctx.ActionsRepositoryIdentity(),
		Token: token,
		Result: data.ActionRunsResponse{
			Runs: []data.ActionRun{{ID: 99, Status: "completed", Conclusion: "success"}},
		},
	})
	model = *updated.(*Model)
	require.Equal(t, "completed", model.Runs[0].Status)
	require.Equal(t, "success", model.Runs[0].Conclusion)
	require.False(t, model.pollScheduled)
}

func TestChangingWorkflowInvalidatesPendingRunsPoll(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	model.Runs = []data.ActionRun{{ID: 99, Status: "in_progress"}}
	require.NotNil(t, model.scheduleRunsPollIfNeeded())
	oldToken := model.pollToken

	model.Workflows = []data.Workflow{{ID: 77, Name: "Deploy", State: "active"}}
	model.workflowCursor = 1
	require.NotNil(t, model.SelectWorkflow())
	require.NotEqual(t, oldToken, model.pollToken)
	require.False(t, model.pollScheduled)
}

func TestActionsRepositoryDefaultsToStartupAndCommandsUseIndependentSelection(t *testing.T) {
	ctx := actionsTestContext("github.com")
	require.Equal(t, "github.com/acme/widgets", ctx.ActionsRepositoryIdentity())

	ctx.SetActionsRepository(repository.Repository{Host: "ghe.example.com", Owner: "octo", Name: "selected"})
	require.Equal(t, "github.com", ctx.GHRepo.Host)
	require.Equal(t, "acme", ctx.GHRepo.Owner)
	require.Equal(t,
		[]string{"run", "watch", "42", "--repo", "ghe.example.com/octo/selected"},
		watchArgs(ctx, 42),
	)
	require.Equal(t,
		[]string{"workflow", "run", "42", "--repo", "ghe.example.com/octo/selected"},
		dispatchArgs(ctx, 42),
	)
}

func TestActionCommandsAreDisabledWithoutRepository(t *testing.T) {
	ctx := actionsTestContext("github.com")
	ctx.GHRepo = nil
	require.Nil(t, watchArgs(ctx, 42))
	require.Nil(t, rerunArgs(ctx, 42))
	require.Nil(t, dispatchArgs(ctx, 42))

	model := NewModel(0, ctx, time.Now())
	model.Runs = []data.ActionRun{{ID: 42}}
	model.Workflows = []data.Workflow{{ID: 7, State: "active"}}
	model.workflowCursor = 1
	model.selectedWorkflowID = 7
	model.Table.SetRows(model.BuildRows())
	require.Nil(t, model.Watch())
	require.Nil(t, model.Rerun())
	require.Nil(t, model.Dispatch())
	require.Nil(t, model.pollRuns(1))
}

func TestRepositorySelectionResetsActionsStateAndRejectsStaleResults(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	model.Runs = []data.ActionRun{{ID: 1, Status: "in_progress"}}
	model.Workflows = []data.Workflow{{ID: 2, Name: "Old"}}
	model.selectedWorkflowID = 2
	model.workflowCursor = 1
	model.workflowListFocused = false
	model.page = 3
	model.PageInfo = &data.PageInfo{HasNextPage: true, EndCursor: "4"}
	model.Table.SetRows(model.BuildRows())
	require.NotNil(t, model.scheduleRunsPollIfNeeded())

	model.repositoryPickerOpen = true
	model.repositoryRequestID = 1
	updated, _ := model.Update(RepositoriesFetchedMsg{RequestID: 1, Repositories: []data.ActionRepository{
		{Host: "github.com", Owner: "other", Name: "new", FullName: "other/new"},
	}})
	model = *updated.(*Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = *updated.(*Model)
	require.NotNil(t, cmd)
	require.Equal(t, "github.com/other/new", ctx.ActionsRepositoryIdentity())
	require.Empty(t, model.Runs)
	require.Empty(t, model.Workflows)
	require.Nil(t, model.PageInfo)
	require.Equal(t, int64(0), model.SelectedWorkflowID())
	require.Equal(t, 0, model.WorkflowCursor())
	require.True(t, model.WorkflowsFocused())
	require.False(t, model.pollScheduled)

	model.LastFetchTaskId = "collision"
	updated, _ = model.Update(RunsFetchedMsg{
		TaskID: "collision", Page: 1, RepositoryIdentity: "github.com/acme/widgets",
		Runs: []data.ActionRun{{ID: 999}},
	})
	require.Empty(t, updated.(*Model).Runs, "a response from the previous repository must be ignored")
}

func TestPollingCommandCapturesSelectedRepository(t *testing.T) {
	ctx := actionsTestContext("github.com")
	ctx.SetActionsRepository(repository.Repository{Host: "ghe.example.com", Owner: "other", Name: "selected"})
	model := NewModel(7, ctx, time.Now())

	original := fetchActionRuns
	t.Cleanup(func() { fetchActionRuns = original })
	var host, owner, name string
	fetchActionRuns = func(gotHost, gotOwner, gotName string, _ int64, _, _ int) (data.ActionRunsResponse, error) {
		host, owner, name = gotHost, gotOwner, gotName
		return data.ActionRunsResponse{}, nil
	}

	msg := model.pollRuns(123)()
	require.Equal(t, "ghe.example.com", host)
	require.Equal(t, "other", owner)
	require.Equal(t, "selected", name)
	polled := msg.(RunsPolledMsg)
	require.Equal(t, "ghe.example.com/other/selected", polled.RepositoryIdentity)
}

func TestRunsAndWorkflowsCaptureSelectedRepository(t *testing.T) {
	ctx := actionsTestContext("github.com")
	ctx.SetActionsRepository(repository.Repository{Host: "ghe.example.com", Owner: "other", Name: "selected"})
	model := NewModel(7, ctx, time.Now())
	model.workflowRefreshNeeded = false

	originalRuns := fetchActionRuns
	originalWorkflows := fetchActionWorkflows
	t.Cleanup(func() {
		fetchActionRuns = originalRuns
		fetchActionWorkflows = originalWorkflows
	})
	var runScope, workflowScope string
	fetchActionRuns = func(host, owner, name string, _ int64, _, _ int) (data.ActionRunsResponse, error) {
		runScope = host + "/" + owner + "/" + name
		return data.ActionRunsResponse{}, nil
	}
	fetchActionWorkflows = func(host, owner, name string) ([]data.Workflow, error) {
		workflowScope = host + "/" + owner + "/" + name
		return nil, nil
	}

	runCommands := model.FetchNextPageSectionRows()
	require.Len(t, runCommands, 3)
	require.NotNil(t, runCommands[1])
	runCommands[1]()
	workflowCommands := model.fetchWorkflowList()
	require.Len(t, workflowCommands, 2)
	require.NotNil(t, workflowCommands[1])
	workflowCommands[1]()
	require.Equal(t, "ghe.example.com/other/selected", runScope)
	require.Equal(t, "ghe.example.com/other/selected", workflowScope)
}

func TestFetchWithoutStartupRepositoryOpensPicker(t *testing.T) {
	ctx := actionsTestContext("github.com")
	ctx.GHRepo = nil
	model, cmd := Fetch(ctx)
	require.True(t, model.RepositoryPickerOpen())
	require.NotNil(t, cmd)
	require.False(t, ctx.HasActionsRepository())
}

func TestRepositoryPickerFiltersNavigatesAndIsNarrowSafe(t *testing.T) {
	ctx := actionsTestContext("github.com")
	ctx.MainContentWidth = 8
	ctx.MainContentHeight = 4
	model := NewModel(0, ctx, time.Now())
	model.repositoryPickerOpen = true
	model.repositories = []data.ActionRepository{
		{Host: "github.com", Owner: "acme", Name: "alpha", FullName: "acme/alpha"},
		{Host: "github.com", Owner: "other", Name: "beta", FullName: "other/beta"},
	}
	model.filterRepositories()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = *updated.(*Model)
	require.Equal(t, 1, model.repositoryCursor)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	model = *updated.(*Model)
	require.Equal(t, 0, model.repositoryCursor)
	updated, _ = model.Update(tea.KeyPressMsg{Text: "beta", Code: 'b'})
	model = *updated.(*Model)
	require.Len(t, model.filteredRepositories, 1)
	require.Equal(t, "beta", model.filteredRepositories[0].Name)
	require.NotPanics(t, func() { _ = model.View() })

	requestID := model.repositoryRequestID
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = *updated.(*Model)
	require.False(t, model.RepositoryPickerOpen())
	require.Greater(t, model.repositoryRequestID, requestID)
}

func TestRepositoryPickerRejectsStaleLoad(t *testing.T) {
	ctx := actionsTestContext("github.com")
	model := NewModel(0, ctx, time.Now())
	model.repositoryPickerOpen = true
	model.repositoriesLoading = true
	model.repositoryRequestID = 2

	updated, _ := model.Update(RepositoriesFetchedMsg{
		RequestID:    1,
		Repositories: []data.ActionRepository{{Host: "github.com", Owner: "stale", Name: "repo", FullName: "stale/repo"}},
	})
	model = *updated.(*Model)
	require.True(t, model.repositoriesLoading)
	require.Empty(t, model.repositories)
}
