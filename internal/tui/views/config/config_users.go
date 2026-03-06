package config

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/your-org/dashboard/internal/tui/client"
)

// ---- internal message types ----

type usersLoadedMsg struct {
	users []client.UserResponse
	err   error
}

type userMutatedMsg struct{ err error }

// ---- mode enum ----

type configUsersMode int

const (
	cfgUsersModeNormal configUsersMode = iota
	cfgUsersModeInputNewUser  // username field active
	cfgUsersModeInputPassword // password field active
	cfgUsersModeInputRole     // role field active
	cfgUsersModeEditRole      // editing existing user role
	cfgUsersModeEditPassword  // editing existing user password
	cfgUsersModeConfirmDelete
)

// ConfigUsersView manages user accounts.
type ConfigUsersView struct {
	c       *client.Client
	users   []client.UserResponse
	loading bool
	errMsg  string
	cursor  int
	mode    configUsersMode
	input   textinput.Model
	confirm string
	// temp storage while creating a new user across multiple input modes
	newUsername string
	newPassword string
	newRole     string
}

// NewConfigUsersView creates a ConfigUsersView.
func NewConfigUsersView(c *client.Client) *ConfigUsersView {
	ti := textinput.New()
	ti.Width = 40
	return &ConfigUsersView{c: c, loading: true, input: ti}
}

// Init implements tea.Model.
func (v *ConfigUsersView) Init() tea.Cmd {
	return v.loadUsers()
}

func (v *ConfigUsersView) loadUsers() tea.Cmd {
	return func() tea.Msg {
		users, err := v.c.GetConfigUsers()
		return usersLoadedMsg{users: users, err: err}
	}
}

func (v *ConfigUsersView) currentUser() *client.UserResponse {
	if v.cursor < 0 || v.cursor >= len(v.users) {
		return nil
	}
	return &v.users[v.cursor]
}

// Update implements tea.Model.
func (v *ConfigUsersView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case usersLoadedMsg:
		v.loading = false
		if m.err != nil {
			v.errMsg = m.err.Error()
		} else {
			v.users = m.users
		}
		return v, nil

	case userMutatedMsg:
		if m.err != nil {
			v.errMsg = m.err.Error()
		}
		v.loading = true
		return v, v.loadUsers()

	case tea.KeyMsg:
		return v.handleKey(m.String(), msg)
	}
	return v, nil
}

func (v *ConfigUsersView) handleKey(key string, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v.mode {
	case cfgUsersModeInputNewUser, cfgUsersModeInputPassword, cfgUsersModeInputRole,
		cfgUsersModeEditRole, cfgUsersModeEditPassword:
		return v.handleInputKey(key, msg)
	case cfgUsersModeConfirmDelete:
		return v.handleConfirmKey(key)
	default:
		return v.handleNormalKey(key)
	}
}

func (v *ConfigUsersView) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		if v.cursor < len(v.users)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "n":
		v.newUsername = ""
		v.newPassword = ""
		v.newRole = "view"
		v.input.SetValue("")
		v.input.Placeholder = "username"
		v.input.EchoMode = textinput.EchoNormal
		v.input.Focus()
		v.mode = cfgUsersModeInputNewUser
		return v, textinput.Blink
	case "e":
		if u := v.currentUser(); u != nil {
			v.input.SetValue(u.Role)
			v.input.Placeholder = "role (view/edit)"
			v.input.EchoMode = textinput.EchoNormal
			v.input.Focus()
			v.mode = cfgUsersModeEditRole
			return v, textinput.Blink
		}
	case "d":
		if u := v.currentUser(); u != nil {
			v.confirm = fmt.Sprintf("Delete user %q? [y/N]", u.Username)
			v.mode = cfgUsersModeConfirmDelete
		}
	}
	return v, nil
}

func (v *ConfigUsersView) handleInputKey(key string, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		v.mode = cfgUsersModeNormal
		return v, nil
	case "enter":
		return v.advanceInputMode()
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return v, cmd
}

func (v *ConfigUsersView) advanceInputMode() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(v.input.Value())
	switch v.mode {
	case cfgUsersModeInputNewUser:
		if value == "" {
			v.mode = cfgUsersModeNormal
			return v, nil
		}
		v.newUsername = value
		v.input.SetValue("")
		v.input.Placeholder = "password"
		v.input.EchoMode = textinput.EchoPassword
		v.mode = cfgUsersModeInputPassword
		return v, nil

	case cfgUsersModeInputPassword:
		v.newPassword = value
		v.input.SetValue("view")
		v.input.Placeholder = "role (view/edit)"
		v.input.EchoMode = textinput.EchoNormal
		v.mode = cfgUsersModeInputRole
		return v, nil

	case cfgUsersModeInputRole:
		role := value
		if role != "edit" {
			role = "view"
		}
		v.mode = cfgUsersModeNormal
		username, password := v.newUsername, v.newPassword
		c := v.c
		return v, func() tea.Msg {
			_, err := c.PostConfigUser(username, password, role)
			return userMutatedMsg{err: err}
		}

	case cfgUsersModeEditRole:
		role := value
		if role != "edit" {
			role = "view"
		}
		// Ask for optional new password next.
		v.newRole = role
		v.input.SetValue("")
		v.input.Placeholder = "new password (leave blank to keep)"
		v.input.EchoMode = textinput.EchoPassword
		v.mode = cfgUsersModeEditPassword
		return v, nil

	case cfgUsersModeEditPassword:
		u := v.currentUser()
		if u == nil {
			v.mode = cfgUsersModeNormal
			return v, nil
		}
		id := u.ID
		role := v.newRole
		password := value
		v.mode = cfgUsersModeNormal
		c := v.c
		return v, func() tea.Msg {
			err := c.PutConfigUser(id, role, password)
			return userMutatedMsg{err: err}
		}
	}
	return v, nil
}

func (v *ConfigUsersView) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	v.mode = cfgUsersModeNormal
	v.confirm = ""
	if key == "y" || key == "Y" {
		u := v.currentUser()
		if u == nil {
			return v, nil
		}
		id := u.ID
		c := v.c
		return v, func() tea.Msg {
			err := c.DeleteConfigUser(id)
			return userMutatedMsg{err: err}
		}
	}
	return v, nil
}

// View implements tea.Model.
func (v *ConfigUsersView) View() string {
	var sb strings.Builder
	sb.WriteString("\n  " + cfgSelectedStyle.Render("Config — Users") + "\n\n")

	if v.errMsg != "" {
		sb.WriteString("  Error: " + v.errMsg + "\n\n")
	}

	// Input prompts.
	switch v.mode {
	case cfgUsersModeInputNewUser:
		sb.WriteString("  New user — username: " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to continue  ·  Esc to cancel") + "\n\n")
	case cfgUsersModeInputPassword:
		sb.WriteString("  New user — password: " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to continue  ·  Esc to cancel") + "\n\n")
	case cfgUsersModeInputRole:
		sb.WriteString("  New user — role (view/edit): " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to confirm  ·  Esc to cancel") + "\n\n")
	case cfgUsersModeEditRole:
		sb.WriteString("  Edit role (view/edit): " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to continue  ·  Esc to cancel") + "\n\n")
	case cfgUsersModeEditPassword:
		sb.WriteString("  New password (blank to keep): " + v.input.View() + "\n")
		sb.WriteString(cfgDimStyle.Render("  Enter to save  ·  Esc to cancel") + "\n\n")
	case cfgUsersModeConfirmDelete:
		sb.WriteString("  " + v.confirm + "\n\n")
	}

	if v.loading {
		sb.WriteString("  Loading…\n")
		sb.WriteString(v.footer())
		return sb.String()
	}
	if len(v.users) == 0 {
		sb.WriteString("  No users. Press n to create one.\n")
		sb.WriteString(v.footer())
		return sb.String()
	}

	for i, u := range v.users {
		prefix := "  "
		name := u.Username
		if i == v.cursor {
			prefix = "> "
			name = cfgSelectedStyle.Render(u.Username)
		}
		roleLabel := cfgDimStyle.Render(fmt.Sprintf("[%s]", u.Role))
		sb.WriteString(fmt.Sprintf("%s%-30s %s\n", prefix, name, roleLabel))
	}

	sb.WriteString(v.footer())
	return sb.String()
}

func (v *ConfigUsersView) footer() string {
	return "\n" + cfgDimStyle.Render("  j/k navigate  ·  n new user  ·  e edit  ·  d delete  ·  Esc back") + "\n"
}
