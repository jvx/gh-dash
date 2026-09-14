package context

import (
	"time"

	tea "charm.land/bubbletea/v2"

	gitm "github.com/aymanbagabas/git-module"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type State = int

const (
	TaskStart State = iota
	TaskFinished
	TaskError
)

type Task struct {
	Id                string
	StartText         string
	FinishedText      string
	PersistCompletion bool
	State             State
	Error             error
	StartTime         time.Time
	FinishedTime      *time.Time
}

type ProgramContext struct {
	GHRepo *repository.Repository
	// ActionsRepo is independent from the local/startup repository and is used
	// only by the Actions view. A nil value falls back to GHRepo.
	ActionsRepo          *repository.Repository
	GitRepo              *gitm.Repository
	RepoPath             string
	RepoUrl              string
	User                 string
	ScreenHeight         int
	ScreenWidth          int
	MainContentWidth     int
	MainContentHeight    int
	DynamicPreviewWidth  int
	DynamicPreviewHeight int    // calculated preview height for bottom mode
	PreviewPosition      string // resolved "right" or "bottom"
	SidebarOpen          bool
	HasDarkBackground    bool
	BackgroundSource     string
	Config               *config.Config
	ConfigFlag           string
	Version              string
	View                 config.ViewType
	Error                error
	StartTask            func(task Task) tea.Cmd
	Theme                theme.Theme
	Styles               Styles
}

func (ctx *ProgramContext) HasGHRepo() bool {
	return ctx.GHRepo != nil && *ctx.GHRepo != (repository.Repository{})
}

func (ctx *ProgramContext) ActionsRepository() *repository.Repository {
	if ctx.ActionsRepo != nil && *ctx.ActionsRepo != (repository.Repository{}) {
		return ctx.ActionsRepo
	}
	if ctx.HasGHRepo() {
		return ctx.GHRepo
	}
	return nil
}

func (ctx *ProgramContext) HasActionsRepository() bool { return ctx.ActionsRepository() != nil }

func (ctx *ProgramContext) SetActionsRepository(repo repository.Repository) {
	ctx.ActionsRepo = &repo
}

func RepositoryIdentity(repo *repository.Repository) string {
	if repo == nil {
		return ""
	}
	return repo.Host + "/" + repo.Owner + "/" + repo.Name
}

func (ctx *ProgramContext) ActionsRepositoryIdentity() string {
	return RepositoryIdentity(ctx.ActionsRepository())
}

func (ctx *ProgramContext) GetViewSectionsConfig() []config.SectionConfig {
	var configs []config.SectionConfig
	switch ctx.View {
	case config.RepoView:
		t := config.RepoView
		configs = append(configs, config.PrsSectionConfig{
			Title:   "Local Branches",
			Filters: "author:@me is:open",
			Limit:   utils.IntPtr(20),
			Type:    &t,
		}.ToSectionConfig())
	case config.NotificationsView:
		for _, cfg := range ctx.Config.NotificationsSections {
			configs = append(configs, cfg.ToSectionConfig())
		}
	case config.PRsView:
		for _, cfg := range ctx.Config.PRSections {
			configs = append(configs, cfg.ToSectionConfig())
		}
	case config.IssuesView:
		for _, cfg := range ctx.Config.IssuesSections {
			configs = append(configs, cfg.ToSectionConfig())
		}
	case config.ActionsView:
		configs = append(configs, config.SectionConfig{Title: "Workflow Runs"})
	}

	if ctx.View == config.ActionsView {
		return configs
	}
	return append([]config.SectionConfig{{Title: ""}}, configs...)
}

func (ctx *ProgramContext) PreviewCursorPosition() tea.Position {
	if ctx.PreviewPosition == "right" {
		return tea.Position{
			X: ctx.MainContentWidth,
			Y: ctx.Styles.Pager.Height,
		}
	}

	return tea.Position{
		X: 0,
		Y: ctx.MainContentHeight,
	}
}
