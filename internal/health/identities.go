package health

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Read only credential source selectors, never secret values or shell expressions.
func sourceSelectors() map[string]string {
	result := map[string]string{}
	home, _ := os.UserHomeDir()
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		root = filepath.Join(home, ".config")
	}
	file, err := os.Open(filepath.Join(root, "fs-log-data/config.env"))
	if err != nil {
		for _, key := range []string{"FS_MCP_AWS_PRODUCTION_CREDENTIALS", "FS_MCP_AWS_STAGING_CREDENTIALS", "FS_MCP_BQ_PRODUCTION_CREDENTIALS", "FS_MCP_BQ_STAGING_CREDENTIALS"} {
			if value := os.Getenv(key); value != "" {
				result[key] = value
			}
		}
		return result
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scan.Text()), "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "FS_MCP_AWS_PRODUCTION_CREDENTIALS" && key != "FS_MCP_AWS_STAGING_CREDENTIALS" && key != "FS_MCP_BQ_PRODUCTION_CREDENTIALS" && key != "FS_MCP_BQ_STAGING_CREDENTIALS" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		result[key] = value
	}
	for _, key := range []string{"FS_MCP_AWS_PRODUCTION_CREDENTIALS", "FS_MCP_AWS_STAGING_CREDENTIALS", "FS_MCP_BQ_PRODUCTION_CREDENTIALS", "FS_MCP_BQ_STAGING_CREDENTIALS"} {
		if value := os.Getenv(key); value != "" {
			result[key] = value
		}
	}
	return result
}
func attachSourceActions(checks []Check, sources map[string]string) {
	for i := range checks {
		c := &checks[i]
		if c.Host != "fs-log-data" || !c.NeedsAttention() {
			continue
		}
		env := strings.ToUpper(c.Scope)
		if env != "PRODUCTION" && env != "STAGING" {
			continue
		}
		if c.Name == "AWS session" && c.Action == nil {
			source := sources["FS_MCP_AWS_"+env+"_CREDENTIALS"]
			if strings.HasPrefix(source, "profile:") && validTarget(strings.TrimPrefix(source, "profile:")) {
				c.Action = &Action{Kind: "aws-login", Target: strings.TrimPrefix(source, "profile:")}
			}
		}
		if c.Name == "bigquery" && strings.Contains(c.Detail, "authentication") && sources["FS_MCP_BQ_"+env+"_CREDENTIALS"] == "adc" {
			c.State = NeedsAuth
			c.Action = &Action{Kind: "gcloud-adc"}
		}
	}
}
