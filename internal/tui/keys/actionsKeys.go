package keys

import (
	"charm.land/bubbles/v2/key"
)

type ActionsKeyMap struct {
	Watch      key.Binding
	Rerun      key.Binding
	Dispatch   key.Binding
	SwitchView key.Binding
}

var ActionsKeys = ActionsKeyMap{
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
		key.WithHelp("d", "dispatch workflow"),
	),
	SwitchView: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "switch view"),
	),
}

func ActionsFullHelp() []key.Binding {
	return []key.Binding{ActionsKeys.Watch, ActionsKeys.Rerun, ActionsKeys.Dispatch, ActionsKeys.SwitchView}
}
