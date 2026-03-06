package views

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// LoginDoneMsg is sent when login succeeds, so the App can pop the login view.
type LoginDoneMsg struct{}

type loginField int

const (
	fieldUsername loginField = iota
	fieldPassword
)

// LoginView is the terminal login screen.
type LoginView struct {
	c       *client.Client
	inputs  [2]textinput.Model
	focused loginField
	errMsg  string
	loading bool
}

// NewLoginView creates a LoginView backed by the given client.
func NewLoginView(c *client.Client) *LoginView {
	user := textinput.New()
	user.Placeholder = "username"
	user.Focus()

	pass := textinput.New()
	pass.Placeholder = "password"
	pass.EchoMode = textinput.EchoPassword
	pass.EchoCharacter = '•'

	return &LoginView{
		c:       c,
		inputs:  [2]textinput.Model{user, pass},
		focused: fieldUsername,
	}
}

type loginResultMsg struct{ err error }

func doLogin(c *client.Client, username, password string) tea.Cmd {
	return func() tea.Msg {
		return loginResultMsg{err: c.Login(username, password)}
	}
}

// Init implements tea.Model.
func (v *LoginView) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (v *LoginView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		if v.loading {
			return v, nil
		}
		switch m.String() {
		case "tab", "down":
			v.focused = (v.focused + 1) % 2
			v.syncFocus()
			return v, nil
		case "shift+tab", "up":
			v.focused = (v.focused + 1) % 2 // only 2 fields; wraps same either way
			v.syncFocus()
			return v, nil
		case "enter":
			if v.focused == fieldUsername {
				v.focused = fieldPassword
				v.syncFocus()
				return v, nil
			}
			// Submit from password field.
			username := v.inputs[fieldUsername].Value()
			password := v.inputs[fieldPassword].Value()
			if username == "" || password == "" {
				v.errMsg = "username and password are required"
				return v, nil
			}
			v.loading = true
			v.errMsg = ""
			return v, doLogin(v.c, username, password)
		}

	case loginResultMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
			return v, nil
		}
		return v, func() tea.Msg { return LoginDoneMsg{} }
	}

	// Forward key events to the focused input.
	var cmd tea.Cmd
	v.inputs[v.focused], cmd = v.inputs[v.focused].Update(msg)
	return v, cmd
}

// View implements tea.Model.
func (v *LoginView) View() string {
	if v.loading {
		return "\n  Logging in…\n"
	}

	s := "\n  Dashboard Login\n\n"
	s += "  Username  " + v.inputs[fieldUsername].View() + "\n"
	s += "  Password  " + v.inputs[fieldPassword].View() + "\n\n"
	s += "  Enter to submit · Tab to switch fields · Ctrl+C to quit\n"
	if v.errMsg != "" {
		s += "\n  Error: " + v.errMsg + "\n"
	}
	return s
}

func (v *LoginView) syncFocus() {
	for i := range v.inputs {
		if loginField(i) == v.focused {
			v.inputs[i].Focus()
		} else {
			v.inputs[i].Blur()
		}
	}
}
