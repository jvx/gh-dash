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

	updated, _ := model.Update(RunsFetchFailedMsg{TaskID: "latest"})
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
		TaskID: "workflows",
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

	updated, cmd := model.Update(WorkflowsFetchedMsg{TaskID: "latest", Workflows: []data.Workflow{{ID: 88, Name: "Current", State: "active"}}})
	model = *updated.(*Model)
	require.NotNil(t, cmd)
	require.Equal(t, int64(0), model.SelectedWorkflowID())
	require.Empty(t, model.Runs)
	require.NotEqual(t, oldRunTaskID, model.LastFetchTaskId)
	require.True(t, model.IsLoading)
}
