package data

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gh "github.com/cli/go-gh/v2/pkg/api"
	"github.com/stretchr/testify/require"
)

type actionsRoundTripper struct{ handler http.Handler }

func (t actionsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}

func newActionsTestClient(t *testing.T, handler http.Handler) *gh.RESTClient {
	t.Helper()
	client, err := gh.NewRESTClient(gh.ClientOptions{
		Host:      "ghe.example.com",
		AuthToken: "test",
		Transport: actionsRoundTripper{handler: handler},
	})
	require.NoError(t, err)
	return client
}

func TestFetchWorkflowRuns(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/widgets/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "25", r.URL.Query().Get("per_page"))
		require.Equal(t, "2", r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":51,"workflow_runs":[{"id":9007199254740993,"run_number":42,"name":"CI","display_title":"Use int64 IDs","status":"completed","conclusion":"success","html_url":"https://ghe.example.com/acme/widgets/actions/runs/9007199254740993"}]}`))
	})

	result, err := fetchActionRuns(newActionsTestClient(t, mux), "acme", "widgets", 0, 25, 2)
	require.NoError(t, err)
	require.Equal(t, 51, result.TotalCount)
	require.True(t, result.HasNextPage)
	require.Len(t, result.Runs, 1)
	require.Equal(t, int64(9007199254740993), result.Runs[0].ID)
	require.Equal(t, "success", result.Runs[0].Conclusion)
	require.Equal(t, "completed", result.Runs[0].Status)
}

func TestFetchWorkflowJobs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/widgets/actions/runs/99/jobs", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "100", r.URL.Query().Get("per_page"))
		require.Equal(t, "1", r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"jobs":[{"id":101,"name":"test","status":"completed","conclusion":"failure"}]}`))
	})

	jobs, err := fetchWorkflowJobs(newActionsTestClient(t, mux), "acme", "widgets", 99)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, int64(101), jobs[0].ID)
	require.Equal(t, "failure", jobs[0].Conclusion)
}

func TestFetchWorkflowJobsPaginates(t *testing.T) {
	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/widgets/actions/runs/99/jobs", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		page := r.URL.Query().Get("page")
		jobs := make([]WorkflowJob, 0)
		if page == "1" {
			for i := 0; i < 100; i++ {
				jobs = append(jobs, WorkflowJob{ID: int64(i + 1), Name: "matrix"})
			}
		} else {
			jobs = append(jobs, WorkflowJob{ID: 101, Name: "final"})
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(workflowJobsAPIResponse{TotalCount: 101, Jobs: jobs}))
	})

	jobs, err := fetchWorkflowJobs(newActionsTestClient(t, mux), "acme", "widgets", 99)
	require.NoError(t, err)
	require.Equal(t, 2, requestCount)
	require.Len(t, jobs, 101)
	require.Equal(t, int64(101), jobs[100].ID)
}

func TestFetchWorkflowRunsClampsLimit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/widgets/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "100", r.URL.Query().Get("per_page"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":150,"workflow_runs":[]}`))
	})

	result, err := fetchActionRuns(newActionsTestClient(t, mux), "acme", "widgets", 0, 200, 1)
	require.NoError(t, err)
	require.True(t, result.HasNextPage)
}

func TestFetchWorkflowRunsForSelectedWorkflow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/widgets/actions/workflows/9007199254740993/runs", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "25", r.URL.Query().Get("per_page"))
		require.Equal(t, "1", r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":44,"name":"Deploy"}]}`))
	})

	result, err := fetchActionRuns(newActionsTestClient(t, mux), "acme", "widgets", 9007199254740993, 25, 1)
	require.NoError(t, err)
	require.Len(t, result.Runs, 1)
	require.Equal(t, "Deploy", result.Runs[0].Name)
}

func TestFetchWorkflowsPaginatesAndPreservesInt64IDs(t *testing.T) {
	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/widgets/actions/workflows", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		require.Equal(t, "100", r.URL.Query().Get("per_page"))
		page := r.URL.Query().Get("page")
		workflows := make([]Workflow, 0)
		if page == "1" {
			for i := 0; i < 100; i++ {
				workflows = append(workflows, Workflow{ID: int64(i + 1), Name: "Workflow"})
			}
		} else {
			workflows = append(workflows, Workflow{ID: 9007199254740993, Name: "Deploy", State: "active"})
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(workflowsAPIResponse{TotalCount: 101, Workflows: workflows}))
	})

	workflows, err := fetchWorkflows(newActionsTestClient(t, mux), "acme", "widgets")
	require.NoError(t, err)
	require.Equal(t, 2, requestCount)
	require.Len(t, workflows, 101)
	require.Equal(t, int64(9007199254740993), workflows[100].ID)
	require.Equal(t, "Deploy", workflows[100].Name)
}

func TestFetchActionRepositoriesPaginatesDeduplicatesAndKeepsEnterpriseIdentity(t *testing.T) {
	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/user/repos", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		require.Equal(t, "owner,collaborator,organization_member", r.URL.Query().Get("affiliation"))
		require.Equal(t, "pushed", r.URL.Query().Get("sort"))
		require.Equal(t, "desc", r.URL.Query().Get("direction"))
		require.Equal(t, "100", r.URL.Query().Get("per_page"))

		var repositories []actionRepositoryAPI
		if r.URL.Query().Get("page") == "1" {
			for i := 0; i < 100; i++ {
				repo := actionRepositoryAPI{
					Name:     fmt.Sprintf("repo-%03d", i),
					FullName: fmt.Sprintf("acme/repo-%03d", i),
					PushedAt: time.Unix(int64(i), 0),
				}
				repo.Owner.Login = "acme"
				repositories = append(repositories, repo)
			}
		} else {
			duplicate := actionRepositoryAPI{Name: "repo-099", FullName: "acme/repo-099", PushedAt: time.Unix(99, 0)}
			duplicate.Owner.Login = "acme"
			newest := actionRepositoryAPI{Name: "newest", FullName: "other/newest", PushedAt: time.Unix(1000, 0)}
			newest.Owner.Login = "other"
			repositories = []actionRepositoryAPI{duplicate, newest}
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(repositories))
	})

	repositories, err := fetchActionRepositories(newActionsTestClient(t, mux), "ghe.example.com")
	require.NoError(t, err)
	require.Equal(t, 2, requestCount)
	require.Len(t, repositories, 101)
	require.Equal(t, ActionRepository{
		Host: "ghe.example.com", Owner: "other", Name: "newest", FullName: "other/newest", PushedAt: time.Unix(1000, 0),
	}, repositories[0])
	require.Equal(t, "ghe.example.com", repositories[100].Host)
}
