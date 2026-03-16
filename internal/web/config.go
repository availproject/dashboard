package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ---- Shared config API types ----

type sourceConfigItem struct {
	ID         int64   `json:"id"`
	TeamID     *int64  `json:"team_id"`
	Purpose    string  `json:"purpose"`
	ConfigMeta *string `json:"config_meta"`
}

type sourceItem struct {
	ID                 int64              `json:"id"`
	SourceType         string             `json:"source_type"`
	ExternalID         string             `json:"external_id"`
	Title              string             `json:"title"`
	URL                *string            `json:"url"`
	SourceMeta         *string            `json:"source_meta"`
	ParentID           *int64             `json:"parent_id"`
	AISuggestedPurpose *string            `json:"ai_suggested_purpose"`
	Status             string             `json:"status"`
	Configs            []sourceConfigItem `json:"configs"`
}

type annotationItem struct {
	ID        int64   `json:"id"`
	TeamID    *int64  `json:"team_id"`
	ItemRef   *string `json:"item_ref"`
	Tier      string  `json:"tier"`
	Content   string  `json:"content"`
	Archived  bool    `json:"archived"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type configAnnotationsResp struct {
	Item []annotationItem `json:"item"`
	Team []annotationItem `json:"team"`
}

type userItem struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// ---- Config root ----

func (d *Deps) configRoot(w http.ResponseWriter, r *http.Request) {
	c := newAPIClient(r, d.APIBase)
	base := buildBase(r, c, 0)
	render(w, "config_root.html", base)
}

// ---- Config Sources ----

type configSourcesPage struct {
	pageBase
	Sources    []sourceItem
	Teams      []apiTeamItem
	TypeFilter string
	Search     string
	Error      string
}

func (d *Deps) configSources(w http.ResponseWriter, r *http.Request) {
	c := newAPIClient(r, d.APIBase)
	base := buildBase(r, c, 0)

	typeFilter := r.URL.Query().Get("type")
	search := r.URL.Query().Get("q")

	url := "/config/sources"
	if typeFilter != "" {
		url += "?source_type=" + typeFilter
	}

	var sources []sourceItem
	var teams []apiTeamItem
	srcErr := c.getJSON(url, &sources)
	_ = c.getJSON("/teams", &teams)

	if search != "" {
		filtered := sources[:0]
		q := strings.ToLower(search)
		for _, s := range sources {
			if strings.Contains(strings.ToLower(s.Title), q) ||
				strings.Contains(strings.ToLower(s.ExternalID), q) {
				filtered = append(filtered, s)
			}
		}
		sources = filtered
	}

	errMsg := ""
	if srcErr != nil {
		errMsg = srcErr.Error()
	}

	render(w, "config_sources.html", configSourcesPage{
		pageBase:   base,
		Sources:    sources,
		Teams:      teams,
		TypeFilter: typeFilter,
		Search:     search,
		Error:      errMsg,
	})
}

func (d *Deps) configSourcesDiscover(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		renderPartial(w, "sync_status.html", syncStatusData{Error: "bad form"})
		return
	}
	target := r.FormValue("target")
	scope := r.FormValue("scope")
	if scope == "" {
		scope = "notion"
	}
	c := newAPIClient(r, d.APIBase)
	var resp struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	if err := c.postJSON("/config/sources/discover", map[string]string{"scope": scope, "target": target}, &resp); err != nil {
		renderPartial(w, "sync_status.html", syncStatusData{Error: err.Error()})
		return
	}
	renderPartial(w, "sync_status.html", syncStatusData{RunID: resp.SyncRunID, Polling: true, Scope: "discover"})
}

func (d *Deps) configSourcesClassify(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		renderPartial(w, "sync_status.html", syncStatusData{Error: "bad form"})
		return
	}
	rawIDs := r.Form["item_ids"]
	var ids []int64
	for _, s := range rawIDs {
		id, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		renderPartial(w, "sync_status.html", syncStatusData{Error: "no items selected"})
		return
	}
	c := newAPIClient(r, d.APIBase)
	var resp struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	if err := c.postJSON("/config/sources/classify", map[string]any{"item_ids": ids}, &resp); err != nil {
		renderPartial(w, "sync_status.html", syncStatusData{Error: err.Error()})
		return
	}
	renderPartial(w, "sync_status.html", syncStatusData{RunID: resp.SyncRunID, Polling: true, Scope: "classify"})
}

func (d *Deps) configSourceUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	purpose := r.FormValue("purpose")
	teamIDStr := r.FormValue("team_id")
	status := r.FormValue("status")

	payload := map[string]any{}
	if purpose != "" {
		payload["purpose"] = purpose
	}
	if status != "" {
		payload["status"] = status
	}
	if teamIDStr != "" {
		if tid, err := strconv.ParseInt(teamIDStr, 10, 64); err == nil {
			payload["team_id"] = tid
		}
	}

	c := newAPIClient(r, d.APIBase)
	var updated sourceItem
	if err := c.putJSON(fmt.Sprintf("/config/sources/%d", id), payload, &updated); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var teams []apiTeamItem
	_ = c.getJSON("/teams", &teams)
	renderPartial(w, "source_row.html", sourceRowData{Source: updated, Teams: teams})
}

type sourceRowData struct {
	Source sourceItem
	Teams  []apiTeamItem
}

func (d *Deps) configSourceDeleteConfig(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	cid, err := strconv.ParseInt(chi_urlparam(r, "cid"), 10, 64)
	if err != nil {
		http.Error(w, "invalid config id", http.StatusBadRequest)
		return
	}
	srcID := chi_urlparam(r, "id")
	c := newAPIClient(r, d.APIBase)
	if err := c.deleteJSON(fmt.Sprintf("/config/sources/%s/config/%d", srcID, cid)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ---- Config Teams ----

type configTeamsPage struct {
	pageBase
	Teams []apiTeamItem
	Error string
}

func (d *Deps) configTeams(w http.ResponseWriter, r *http.Request) {
	c := newAPIClient(r, d.APIBase)
	base := buildBase(r, c, 0)
	var teams []apiTeamItem
	err := c.getJSON("/teams", &teams)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	render(w, "config_teams.html", configTeamsPage{pageBase: base, Teams: teams, Error: errMsg})
}

func (d *Deps) configTeamCreate(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	c := newAPIClient(r, d.APIBase)
	var team apiTeamItem
	if err := c.postJSON("/config/teams", map[string]string{"name": name}, &team); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Return updated teams list.
	d.configTeams(w, r)
}

func (d *Deps) configTeamUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	c := newAPIClient(r, d.APIBase)
	if err := c.putJSON(fmt.Sprintf("/config/teams/%d", id), map[string]string{"name": r.FormValue("name")}, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configTeams(w, r)
}

func (d *Deps) configTeamDelete(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	c := newAPIClient(r, d.APIBase)
	if err := c.deleteJSON(fmt.Sprintf("/config/teams/%d", id)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configTeams(w, r)
}

func (d *Deps) configMemberAdd(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	teamID, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	payload := map[string]any{"display_name": r.FormValue("display_name")}
	if gh := r.FormValue("github_username"); gh != "" {
		payload["github_username"] = gh
	}
	if nu := r.FormValue("notion_user_id"); nu != "" {
		payload["notion_user_id"] = nu
	}
	c := newAPIClient(r, d.APIBase)
	if err := c.postJSON(fmt.Sprintf("/config/teams/%d/members", teamID), payload, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configTeams(w, r)
}

func (d *Deps) configMemberUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	payload := map[string]any{"display_name": r.FormValue("display_name")}
	if gh := r.FormValue("github_username"); gh != "" {
		payload["github_username"] = gh
	}
	if nu := r.FormValue("notion_user_id"); nu != "" {
		payload["notion_user_id"] = nu
	}
	c := newAPIClient(r, d.APIBase)
	if err := c.putJSON(fmt.Sprintf("/config/members/%d", id), payload, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configTeams(w, r)
}

func (d *Deps) configMemberDelete(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	c := newAPIClient(r, d.APIBase)
	if err := c.deleteJSON(fmt.Sprintf("/config/members/%d", id)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configTeams(w, r)
}

// ---- Config Users ----

type configUsersPage struct {
	pageBase
	Users []userItem
	Error string
}

func (d *Deps) configUsers(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	c := newAPIClient(r, d.APIBase)
	base := buildBase(r, c, 0)
	var users []userItem
	err := c.getJSON("/config/users", &users)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	render(w, "config_users.html", configUsersPage{pageBase: base, Users: users, Error: errMsg})
}

func (d *Deps) configUserCreate(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	c := newAPIClient(r, d.APIBase)
	if err := c.postJSON("/config/users", map[string]string{
		"username": r.FormValue("username"),
		"password": r.FormValue("password"),
		"role":     r.FormValue("role"),
	}, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configUsers(w, r)
}

func (d *Deps) configUserUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	payload := map[string]string{}
	if role := r.FormValue("role"); role != "" {
		payload["role"] = role
	}
	if pw := r.FormValue("password"); pw != "" {
		payload["password"] = pw
	}
	c := newAPIClient(r, d.APIBase)
	if err := c.putJSON(fmt.Sprintf("/config/users/%d", id), payload, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configUsers(w, r)
}

func (d *Deps) configUserDelete(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	c := newAPIClient(r, d.APIBase)
	if err := c.deleteJSON(fmt.Sprintf("/config/users/%d", id)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configUsers(w, r)
}

// ---- Config Annotations ----

type configAnnotationsPage struct {
	pageBase
	Annotations configAnnotationsResp
	Teams       []apiTeamItem
	Error       string
}

func (d *Deps) configAnnotations(w http.ResponseWriter, r *http.Request) {
	c := newAPIClient(r, d.APIBase)
	base := buildBase(r, c, 0)
	var anns configAnnotationsResp
	var teams []apiTeamItem
	err := c.getJSON("/config/annotations", &anns)
	_ = c.getJSON("/teams", &teams)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	render(w, "config_annotations.html", configAnnotationsPage{
		pageBase:    base,
		Annotations: anns,
		Teams:       teams,
		Error:       errMsg,
	})
}

func (d *Deps) configAnnotationCreate(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	payload := map[string]any{
		"tier":    r.FormValue("tier"),
		"content": r.FormValue("content"),
	}
	if tid := r.FormValue("team_id"); tid != "" {
		if id, err := strconv.ParseInt(tid, 10, 64); err == nil {
			payload["team_id"] = id
		}
	}
	if ir := r.FormValue("item_ref"); ir != "" {
		payload["item_ref"] = ir
	}
	c := newAPIClient(r, d.APIBase)
	if err := c.postJSON("/config/annotations", payload, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configAnnotations(w, r)
}

func (d *Deps) configAnnotationUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	c := newAPIClient(r, d.APIBase)
	if err := c.putJSON(fmt.Sprintf("/config/annotations/%d", id), map[string]string{"content": r.FormValue("content")}, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configAnnotations(w, r)
}

func (d *Deps) configAnnotationDelete(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi_urlparam(r, "id"), 10, 64)
	c := newAPIClient(r, d.APIBase)
	if err := c.deleteJSON(fmt.Sprintf("/config/annotations/%d", id)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.configAnnotations(w, r)
}

// ---- Config Admin ----

type configAdminPage struct {
	pageBase
}

func (d *Deps) configAdmin(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	c := newAPIClient(r, d.APIBase)
	base := buildBase(r, c, 0)
	render(w, "config_admin.html", configAdminPage{pageBase: base})
}

func (d *Deps) configAdminAutotag(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	c := newAPIClient(r, d.APIBase)
	var resp struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	if err := c.getJSON("/admin/autotag", &resp); err != nil {
		renderPartial(w, "sync_status.html", syncStatusData{Error: err.Error()})
		return
	}
	renderPartial(w, "sync_status.html", syncStatusData{RunID: resp.SyncRunID, Polling: true, Scope: "autotag"})
}

func (d *Deps) configAdminClearCache(w http.ResponseWriter, r *http.Request) {
	if !requireEditRole(w, r) {
		return
	}
	pipeline := r.URL.Query().Get("pipeline")
	c := newAPIClient(r, d.APIBase)
	path := "/admin/ai-cache"
	if pipeline != "" {
		path += "?pipeline=" + pipeline
	}
	var resp struct {
		Deleted int `json:"deleted"`
	}
	if err := c.doJSON(http.MethodDelete, path, nil, &resp); err != nil {
		renderPartial(w, "flash.html", flashData{Type: "error", Message: err.Error()})
		return
	}
	renderPartial(w, "flash.html", flashData{
		Type:    "success",
		Message: fmt.Sprintf("Deleted %d cache entries", resp.Deleted),
	})
}

type flashData struct {
	Type    string
	Message string
}
