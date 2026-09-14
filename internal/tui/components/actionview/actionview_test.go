package actionview

import (
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func TestJobsRejectStaleRepositoryAndResetPreview(t *testing.T) {
	ctx := &context.ProgramContext{
		GHRepo: &repository.Repository{Host: "github.com", Owner: "acme", Name: "old"},
	}
	model := NewModel(ctx)
	model.run = &data.ActionRun{ID: 42}
	model.repositoryIdentity = "github.com/acme/old"
	model.loading = true

	ctx.SetActionsRepository(repository.Repository{Host: "ghe.example.com", Owner: "acme", Name: "new"})
	model.SetJobs(JobsFetchedMsg{
		RunID: 42, RepositoryIdentity: "github.com/acme/old",
		Jobs: []data.WorkflowJob{{ID: 1, Name: "stale"}},
	})
	require.True(t, model.loading)
	require.Empty(t, model.jobs)

	model.Reset()
	require.Nil(t, model.run)
	require.Empty(t, model.jobs)
	require.False(t, model.loading)
	require.Empty(t, model.repositoryIdentity)
}

func TestSetRunCapturesEnterpriseRepository(t *testing.T) {
	ctx := &context.ProgramContext{
		ActionsRepo: &repository.Repository{Host: "ghe.example.com", Owner: "acme", Name: "selected"},
	}
	model := NewModel(ctx)
	original := fetchWorkflowJobs
	t.Cleanup(func() { fetchWorkflowJobs = original })
	var host, owner, name string
	fetchWorkflowJobs = func(gotHost, gotOwner, gotName string, _ int64) ([]data.WorkflowJob, error) {
		host, owner, name = gotHost, gotOwner, gotName
		return nil, nil
	}

	cmd := model.SetRun(&data.ActionRun{ID: 42})
	require.NotNil(t, cmd)
	msg := cmd().(JobsFetchedMsg)
	require.Equal(t, "ghe.example.com", host)
	require.Equal(t, "acme", owner)
	require.Equal(t, "selected", name)
	require.Equal(t, "ghe.example.com/acme/selected", msg.RepositoryIdentity)
}
