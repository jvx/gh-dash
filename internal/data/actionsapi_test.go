package data

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

	result, err := fetchActionRuns(newActionsTestClient(t, mux), "acme", "widgets", 25, 2)
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

	result, err := fetchActionRuns(newActionsTestClient(t, mux), "acme", "widgets", 200, 1)
	require.NoError(t, err)
	require.True(t, result.HasNextPage)
}
