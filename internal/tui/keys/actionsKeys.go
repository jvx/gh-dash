package keys

import (
	"charm.land/bubbles/v2/key"
)

type ActionsKeyMap struct {
	ChooseRepository key.Binding
	FocusWorkflows   key.Binding
	FocusRuns        key.Binding
	SelectWorkflow   key.Binding
	Watch            key.Binding
	Rerun            key.Binding
	Dispatch         key.Binding
	SwitchView       key.Binding
}

var ActionsKeys = ActionsKeyMap{
	ChooseRepository: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "choose repository"),
	),
	FocusWorkflows: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "focus workflows"),
	),
	FocusRuns: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "focus runs"),
	),
	SelectWorkflow: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select workflow"),
	),
	Watch: key.NewBinding(
		key.WithKeys("w"),
		key.WithHelp("w", "watch run"),
	),
	Rerun: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "rerun"),
	),
	Dispatch: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "run selected workflow"),
	),
	SwitchView: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "switch view"),
	),
}

func ActionsFullHelp() []key.Binding {
	return []key.Binding{
		ActionsKeys.ChooseRepository,
		ActionsKeys.FocusWorkflows,
		ActionsKeys.FocusRuns,
		ActionsKeys.SelectWorkflow,
		ActionsKeys.Watch,
		ActionsKeys.Rerun,
		ActionsKeys.Dispatch,
		ActionsKeys.SwitchView,
	}
}
