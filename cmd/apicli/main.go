// apicli is a diagnostic CLI for exercising the dashboard server API.
//
// Usage:
//
//	go run ./cmd/apicli <command> [args...]
//
// Commands:
//
//	login   <username> <password>          -- get a token (printed to stdout)
//	sources                                -- list catalogue items
//	classify <id> [id...]                  -- classify catalogue items by ID
//	run <run_id>                           -- poll a sync run until terminal
//	discover <scope> <target>              -- start a discovery run and poll it
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultAddr = "http://localhost:8081"

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	addr := defaultAddr
	if v := os.Getenv("DASHBOARD_ADDR"); v != "" {
		addr = v
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "login":
		if len(args) != 2 {
			fatalf("usage: apicli login <username> <password>\n")
		}
		doLogin(addr, args[0], args[1])

	case "sources":
		tok := requireToken()
		doSources(addr, tok)

	case "classify":
		if len(args) == 0 {
			fatalf("usage: apicli classify <id> [id...]\n")
		}
		tok := requireToken()
		ids := parseIDs(args)
		doClassify(addr, tok, ids)

	case "run":
		if len(args) != 1 {
			fatalf("usage: apicli run <run_id>\n")
		}
		tok := requireToken()
		runID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			fatalf("invalid run_id: %v\n", err)
		}
		pollRun(addr, tok, runID)

	case "discover":
		if len(args) != 2 {
			fatalf("usage: apicli discover <scope> <target>\n")
		}
		tok := requireToken()
		doDiscover(addr, tok, args[0], args[1])

	case "cache-clear":
		tok := requireToken()
		pipeline := ""
		if len(args) > 0 {
			pipeline = args[0]
		}
		doCacheClear(addr, tok, pipeline)

	default:
		usage()
	}
}

// ---- commands ----

func doLogin(addr, user, pass string) {
	body := map[string]string{"username": user, "password": pass}
	resp := post(addr+"/auth/login", "", body)
	var out struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	decodeOrDie(resp, &out)
	fmt.Printf("token: %s\n\nrefresh_token: %s\n", out.Token, out.RefreshToken)
	fmt.Fprintf(os.Stderr, "\nhint: export DASHBOARD_TOKEN='%s'\n", out.Token)
}

func doSources(addr, tok string) {
	resp := get(addr+"/config/sources", tok)
	var items []map[string]any
	decodeOrDie(resp, &items)
	fmt.Printf("%-6s  %-16s  %-10s  %-40s  %s\n", "ID", "TYPE", "STATUS", "TITLE", "AI_SUGGESTION")
	for _, it := range items {
		id := fmt.Sprintf("%.0f", it["id"])
		typ := str(it["source_type"])
		status := str(it["status"])
		title := truncate(str(it["title"]), 40)
		ai := ""
		if v, ok := it["ai_suggested_purpose"]; ok && v != nil {
			ai = fmt.Sprint(v)
		}
		fmt.Printf("%-6s  %-16s  %-10s  %-40s  %s\n", id, typ, status, title, ai)
	}
	fmt.Printf("\n%d item(s)\n", len(items))
}

func doClassify(addr, tok string, ids []int64) {
	body := map[string]any{"item_ids": ids}
	resp := post(addr+"/config/sources/classify", tok, body)
	var out struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	decodeOrDie(resp, &out)
	fmt.Printf("classify run started: run_id=%d\n", out.SyncRunID)
	pollRun(addr, tok, out.SyncRunID)
}

func doCacheClear(addr, tok, pipeline string) {
	url := addr + "/admin/ai-cache"
	if pipeline != "" {
		url += "?pipeline=" + pipeline
	}
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		fatalf("build request: %v\n", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatalf("DELETE %s: %v\n", url, err)
	}
	checkStatus(resp, url)
	var out map[string]any
	decodeOrDie(resp, &out)
	fmt.Printf("deleted %v cache entry(s)\n", out["deleted"])
}

func doDiscover(addr, tok, scope, target string) {
	body := map[string]string{"scope": scope, "target": target}
	resp := post(addr+"/config/sources/discover", tok, body)
	var out struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	decodeOrDie(resp, &out)
	fmt.Printf("discover run started: run_id=%d\n", out.SyncRunID)
	pollRun(addr, tok, out.SyncRunID)
}

func pollRun(addr, tok string, runID int64) {
	for {
		resp := get(fmt.Sprintf("%s/sync/%d", addr, runID), tok)
		var run struct {
			ID     int64   `json:"ID"`
			Status string  `json:"Status"`
			Scope  string  `json:"Scope"`
			Error  *string `json:"Error"`
		}
		decodeOrDie(resp, &run)
		fmt.Printf("  run %d  scope=%-20s  status=%s", run.ID, run.Scope, run.Status)
		if run.Error != nil {
			fmt.Printf("  error=%s", *run.Error)
		}
		fmt.Println()
		switch run.Status {
		case "completed", "done":
			fmt.Println("done.")
			return
		case "failed", "error":
			fmt.Fprintln(os.Stderr, "run failed.")
			os.Exit(1)
		}
		time.Sleep(2 * time.Second)
	}
}

// ---- helpers ----

func get(url, tok string) *http.Response {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fatalf("build request: %v\n", err)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatalf("GET %s: %v\n", url, err)
	}
	checkStatus(resp, url)
	return resp
}

func post(url, tok string, body any) *http.Response {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		fatalf("build request: %v\n", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatalf("POST %s: %v\n", url, err)
	}
	checkStatus(resp, url)
	return resp
}

func checkStatus(resp *http.Response, url string) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fatalf("server %d from %s: %s\n", resp.StatusCode, url, string(body))
	}
}

func decodeOrDie(resp *http.Response, v any) {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		fatalf("decode response: %v\n", err)
	}
}

func requireToken() string {
	tok := os.Getenv("DASHBOARD_TOKEN")
	if tok == "" {
		fatalf("DASHBOARD_TOKEN not set — run 'apicli login' first\n")
	}
	return tok
}

func parseIDs(args []string) []int64 {
	var ids []int64
	for _, a := range args {
		id, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			fatalf("invalid id %q: %v\n", a, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format, args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(`
usage: apicli <command> [args]

commands:
  login   <username> <password>    authenticate and print token
  sources                          list all catalogue items with AI suggestions
  classify <id> [id...]            classify items by ID and poll until done
  run <run_id>                     poll a sync run until terminal
  discover <scope> <target>        start discovery and poll until done
  cache-clear [pipeline]           delete AI cache entries (all, or one pipeline)

env:
  DASHBOARD_ADDR   server address (default http://localhost:8081)
  DASHBOARD_TOKEN  bearer token (set after login)
`))
	os.Exit(1)
}
