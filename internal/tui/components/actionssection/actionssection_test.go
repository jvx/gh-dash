package actionssection

import (
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
		Config: &cfg,
		GHRepo: &repository.Repository{Host: host, Owner: "acme", Name: "widgets"},
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
		[]string{"workflow", "run", "--repo", "ghe.example.com/acme/widgets"},
		dispatchArgs(ctx),
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
