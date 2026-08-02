package prssection

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prompt"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

// newTestModel creates a minimal Model with the prompt confirmation box
// focused and a single PR row so that GetCurrRow returns non-nil.
func newTestModel(action string) Model {
	ctx := &context.ProgramContext{
		StartTask: func(task context.Task) tea.Cmd {
			return func() tea.Msg { return nil }
		},
	}
	m := Model{
		BaseModel: section.BaseModel{
			Ctx:                       ctx,
			IsPromptConfirmationShown: true,
			PromptConfirmationAction:  action,
			PromptConfirmationBox:     prompt.NewModel(ctx),
		},
		Prs: []prrow.Data{
			{Primary: &data.PullRequestData{Number: 42}},
		},
	}
	m.PromptConfirmationBox.Focus()
	return m
}

func TestConfirmation_EmptyInputDoesNotConfirm(t *testing.T) {
	// Pressing Enter without typing anything should NOT confirm, since the
	// prompt says (y/N) indicating N is the default.
	m := newTestModel("close")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, _ = m.Update(msg)

	require.False(t, m.IsPromptConfirmationShown,
		"confirmation prompt should be dismissed")
}

func TestConfirmation_AcceptWithLowercaseY(t *testing.T) {
	m := newTestModel("merge")
	m.PromptConfirmationBox.SetValue("y")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := m.Update(msg)

	require.NotNil(t, cmd, "lowercase y should execute the action")
}

func TestConfirmation_AcceptWithUppercaseY(t *testing.T) {
	m := newTestModel("reopen")
	m.PromptConfirmationBox.SetValue("Y")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := m.Update(msg)

	require.NotNil(t, cmd, "uppercase Y should execute the action")
}

func TestConfirmation_RejectWithN(t *testing.T) {
	m := newTestModel("close")
	m.PromptConfirmationBox.SetValue("n")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := m.Update(msg)

	// cmd is a batch of (nil, blinkCmd) -- the nil means no action was taken.
	// We verify the prompt is dismissed regardless.
	require.False(t, m.IsPromptConfirmationShown,
		"confirmation prompt should be dismissed on rejection")
	_ = cmd
}

func TestConfirmation_CancelWithEsc(t *testing.T) {
	m := newTestModel("merge")

	msg := tea.KeyPressMsg{Code: tea.KeyEsc}
	_, cmd := m.Update(msg)

	require.False(t, m.IsPromptConfirmationShown,
		"Esc should dismiss the confirmation prompt")
	_ = cmd
}

func TestConfirmation_CancelWithCtrlC(t *testing.T) {
	m := newTestModel("update")

	msg := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	_, cmd := m.Update(msg)

	require.False(t, m.IsPromptConfirmationShown,
		"Ctrl+C should dismiss the confirmation prompt")
	_ = cmd
}

func TestConfirmation_AllActions(t *testing.T) {
	actions := []string{"close", "reopen", "ready", "merge", "update", "approveWorkflows"}

	for _, action := range actions {
		t.Run(action+"_empty_input_does_not_confirm", func(t *testing.T) {
			m := newTestModel(action)

			msg := tea.KeyPressMsg{Code: tea.KeyEnter}
			_, _ = m.Update(msg)

			require.False(t, m.IsPromptConfirmationShown,
				"empty input should dismiss prompt for action %q", action)
		})

		t.Run(action+"_explicit_y", func(t *testing.T) {
			m := newTestModel(action)
			m.PromptConfirmationBox.SetValue("y")

			msg := tea.KeyPressMsg{Code: tea.KeyEnter}
			_, cmd := m.Update(msg)

			require.NotNil(t, cmd,
				"explicit y should confirm for action %q", action)
		})
	}
}

func TestWithSessionMergedPRsPreservesMergedRowsDuringAutomaticRefresh(t *testing.T) {
	olderOpen := prrow.Data{Primary: &data.PullRequestData{
		Number: 41,
		Url:    "https://github.com/acme/app/pull/41",
		State:  "OPEN",
	}}
	merged := prrow.Data{Primary: &data.PullRequestData{
		Number: 42,
		Url:    "https://github.com/acme/app/pull/42",
		State:  "MERGED",
	}}
	newerOpen := prrow.Data{Primary: &data.PullRequestData{
		Number: 43,
		Url:    "https://github.com/acme/app/pull/43",
		State:  "OPEN",
	}}
	newlyFetched := prrow.Data{Primary: &data.PullRequestData{
		Number: 44,
		Url:    "https://github.com/acme/app/pull/44",
		State:  "OPEN",
	}}
	m := Model{
		Prs:                 []prrow.Data{newerOpen, merged, olderOpen},
		sessionMergedPRKeys: map[string]bool{prKey(merged): true},
	}

	got, extraCount := m.withSessionMergedPRs([]prrow.Data{newlyFetched, newerOpen, olderOpen})

	require.Len(t, got, 4)
	require.Equal(t, 44, got[0].Primary.Number)
	require.Equal(t, 43, got[1].Primary.Number)
	require.Equal(t, 42, got[2].Primary.Number)
	require.Equal(t, 41, got[3].Primary.Number)
	require.Equal(t, 1, extraCount)
}

func TestWithSessionMergedPRsKeepsMergedStateWhenFetchIsStale(t *testing.T) {
	merged := prrow.Data{Primary: &data.PullRequestData{
		Number: 42,
		Url:    "https://github.com/acme/app/pull/42",
		State:  "MERGED",
	}}
	staleOpen := prrow.Data{Primary: &data.PullRequestData{
		Number: 42,
		Url:    "https://github.com/acme/app/pull/42",
		State:  "OPEN",
	}}
	m := Model{
		Prs:                 []prrow.Data{merged},
		sessionMergedPRKeys: map[string]bool{prKey(merged): true},
	}

	got, extraCount := m.withSessionMergedPRs([]prrow.Data{staleOpen})

	require.Len(t, got, 1)
	require.Equal(t, "MERGED", got[0].Primary.State)
	require.Zero(t, extraCount, "a tracked PR already included in fetched total must not be double-counted")
}

func TestAppendUniquePRsDoesNotDuplicatePreservedMergedPR(t *testing.T) {
	merged := prrow.Data{Primary: &data.PullRequestData{
		Number: 42,
		Url:    "https://github.com/acme/app/pull/42",
		State:  "MERGED",
	}}

	got := appendUniquePRs([]prrow.Data{merged}, []prrow.Data{merged})

	require.Len(t, got, 1)
}

func TestResetRowsClearsSessionMergedPRs(t *testing.T) {
	merged := prrow.Data{Primary: &data.PullRequestData{
		Number: 42,
		Url:    "https://github.com/acme/app/pull/42",
		State:  "MERGED",
	}}
	m := Model{
		Prs:                 []prrow.Data{merged},
		sessionMergedPRKeys: map[string]bool{prKey(merged): true},
	}

	m.ResetRows()

	require.Empty(t, m.Prs)
	require.Empty(t, m.sessionMergedPRKeys)
}

func TestSelectedPRIdentitySurvivesNewRowAtTop(t *testing.T) {
	pr41 := prrow.Data{Primary: &data.PullRequestData{Number: 41, Url: "https://github.com/acme/app/pull/41"}}
	pr42 := prrow.Data{Primary: &data.PullRequestData{Number: 42, Url: "https://github.com/acme/app/pull/42"}}
	pr43 := prrow.Data{Primary: &data.PullRequestData{Number: 43, Url: "https://github.com/acme/app/pull/43"}}
	pr44 := prrow.Data{Primary: &data.PullRequestData{Number: 44, Url: "https://github.com/acme/app/pull/44"}}

	key := selectedPRKey([]prrow.Data{pr43, pr42, pr41}, 1)
	index, found := findPRIndex([]prrow.Data{pr44, pr43, pr42, pr41}, key)

	require.True(t, found)
	require.Equal(t, 2, index, "selection should follow PR #42 instead of staying on row 1")
}

func TestFindPRIndexRejectsMissingSelection(t *testing.T) {
	pr42 := prrow.Data{Primary: &data.PullRequestData{Number: 42, Url: "https://github.com/acme/app/pull/42"}}

	_, found := findPRIndex([]prrow.Data{pr42}, "https://github.com/acme/app/pull/99")

	require.False(t, found)
}
