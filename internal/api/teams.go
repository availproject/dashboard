package api

import "net/http"

type listTeamsMemberItem struct {
	ID             int64   `json:"id"`
	DisplayName    string  `json:"display_name"`
	GithubUsername *string `json:"github_username"`
	NotionUserID   *string `json:"notion_user_id"`
}

type listTeamsItem struct {
	ID      int64                 `json:"id"`
	Name    string                `json:"name"`
	Members []listTeamsMemberItem `json:"members"`
}

func (d *Deps) handleListTeams(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teams, err := d.Store.ListTeams(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list teams: "+err.Error())
		return
	}

	result := make([]listTeamsItem, 0, len(teams))
	for _, t := range teams {
		members, err := d.Store.GetTeamMembers(ctx, t.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "get members: "+err.Error())
			return
		}

		memberItems := make([]listTeamsMemberItem, 0, len(members))
		for _, m := range members {
			item := listTeamsMemberItem{
				ID:          m.ID,
				DisplayName: m.Name,
			}
			if m.GithubLogin.Valid {
				item.GithubUsername = &m.GithubLogin.String
			}
			if m.NotionUserID.Valid {
				item.NotionUserID = &m.NotionUserID.String
			}
			memberItems = append(memberItems, item)
		}

		result = append(result, listTeamsItem{
			ID:      t.ID,
			Name:    t.Name,
			Members: memberItems,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

func (d *Deps) handleTeamSprint(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (d *Deps) handleTeamGoals(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (d *Deps) handleTeamWorkload(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (d *Deps) handleTeamVelocity(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (d *Deps) handleTeamMetrics(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}
