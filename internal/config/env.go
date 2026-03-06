package config

import (
	"os"
	"strings"
)

var envVars = map[string]string{}

func LoadEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		envVars[key] = val
	}
	return nil
}

func get(key string) string {
	if v, ok := envVars[key]; ok {
		return v
	}
	return os.Getenv(key)
}

func GitHubToken() string    { return get("GITHUB_TOKEN") }
func NotionToken() string    { return get("NOTION_TOKEN") }
func GrafanaToken() string   { return get("GRAFANA_TOKEN") }
func GrafanaBaseURL() string { return get("GRAFANA_BASE_URL") }
func PostHogAPIKey() string  { return get("POSTHOG_API_KEY") }
func PostHogHost() string    { return get("POSTHOG_HOST") }
func SignozAPIKey() string   { return get("SIGNOZ_API_KEY") }
func SignozBaseURL() string  { return get("SIGNOZ_BASE_URL") }
