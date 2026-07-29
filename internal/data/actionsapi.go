package data

import (
	"fmt"
	"time"

	gh "github.com/cli/go-gh/v2/pkg/api"
)

// ActionRun is a repository-scoped GitHub Actions workflow run.
type ActionRun struct {
	ID           int64     `json:"id"`
	RunNumber    int       `json:"run_number"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	Event        string    `json:"event"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Actor        struct {
		Login string `json:"login"`
	} `json:"actor"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// Workflow is a repository-scoped GitHub Actions workflow.
type Workflow struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r ActionRun) GetRepoNameWithOwner() string { return r.Repository.FullName }
func (r ActionRun) GetTitle() string             { return r.DisplayTitle }
func (r ActionRun) GetNumber() int               { return r.RunNumber }
func (r ActionRun) GetUrl() string               { return r.HTMLURL }
func (r ActionRun) GetUpdatedAt() time.Time      { return r.UpdatedAt }

type WorkflowJob struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	HTMLURL     string    `json:"html_url"`
}

type ActionRunsResponse struct {
	Runs        []ActionRun
	TotalCount  int
	Page        int
	HasNextPage bool
}

type workflowRunsAPIResponse struct {
	TotalCount int         `json:"total_count"`
	ActionRuns []ActionRun `json:"workflow_runs"`
}

type workflowsAPIResponse struct {
	TotalCount int        `json:"total_count"`
	Workflows  []Workflow `json:"workflows"`
}

type workflowJobsAPIResponse struct {
	TotalCount int           `json:"total_count"`
	Jobs       []WorkflowJob `json:"jobs"`
}

func actionsRESTClient(host string) (*gh.RESTClient, error) {
	return gh.NewRESTClient(gh.ClientOptions{Host: host})
}

func fetchActionRuns(client *gh.RESTClient, owner, repo string, workflowID int64, limit, page int) (ActionRunsResponse, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	var response workflowRunsAPIResponse
	path := fmt.Sprintf("repos/%s/%s/actions/runs?per_page=%d&page=%d", owner, repo, limit, page)
	if workflowID != 0 {
		path = fmt.Sprintf("repos/%s/%s/actions/workflows/%d/runs?per_page=%d&page=%d", owner, repo, workflowID, limit, page)
	}
	if err := client.Get(path, &response); err != nil {
		return ActionRunsResponse{}, err
	}
	return ActionRunsResponse{
		Runs:        response.ActionRuns,
		TotalCount:  response.TotalCount,
		Page:        page,
		HasNextPage: page*limit < response.TotalCount,
	}, nil
}

func FetchActionRuns(host, owner, repo string, workflowID int64, limit, page int) (ActionRunsResponse, error) {
	client, err := actionsRESTClient(host)
	if err != nil {
		return ActionRunsResponse{}, err
	}
	return fetchActionRuns(client, owner, repo, workflowID, limit, page)
}

func fetchWorkflows(client *gh.RESTClient, owner, repo string) ([]Workflow, error) {
	workflows := make([]Workflow, 0)
	for page := 1; ; page++ {
		var response workflowsAPIResponse
		path := fmt.Sprintf("repos/%s/%s/actions/workflows?per_page=100&page=%d", owner, repo, page)
		if err := client.Get(path, &response); err != nil {
			return nil, err
		}
		workflows = append(workflows, response.Workflows...)
		if len(response.Workflows) < 100 || len(workflows) >= response.TotalCount {
			return workflows, nil
		}
	}
}

func FetchWorkflows(host, owner, repo string) ([]Workflow, error) {
	client, err := actionsRESTClient(host)
	if err != nil {
		return nil, err
	}
	return fetchWorkflows(client, owner, repo)
}

func fetchWorkflowJobs(client *gh.RESTClient, owner, repo string, runID int64) ([]WorkflowJob, error) {
	jobs := make([]WorkflowJob, 0)
	for page := 1; ; page++ {
		var response workflowJobsAPIResponse
		path := fmt.Sprintf("repos/%s/%s/actions/runs/%d/jobs?per_page=100&page=%d", owner, repo, runID, page)
		if err := client.Get(path, &response); err != nil {
			return nil, err
		}
		jobs = append(jobs, response.Jobs...)
		if len(response.Jobs) < 100 || len(jobs) >= response.TotalCount {
			return jobs, nil
		}
	}
}

func FetchWorkflowJobs(host, owner, repo string, runID int64) ([]WorkflowJob, error) {
	client, err := actionsRESTClient(host)
	if err != nil {
		return nil, err
	}
	return fetchWorkflowJobs(client, owner, repo, runID)
}
