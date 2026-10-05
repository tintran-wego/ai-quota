package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexPaginationAndAuthInventory(t *testing.T) {
	dir := t.TempDir()
	script := `#!/usr/bin/python3
import sys,json
if len(sys.argv)>1 and sys.argv[1]=='mcp':
 print(json.dumps({'transport':{'type':'streamable_http','url':'https://example.invalid/mcp'}}));sys.exit(0)
for line in sys.stdin:
 r=json.loads(line);method=r['method'];p=r.get('params',{})
 if method=='initialized':continue
 if method=='initialize':result={}
 elif method=='mcpServerStatus/list':
  if 'cursor' in p and p['cursor']=='':
   print(json.dumps({'id':r['id'],'error':{'message':'invalid cursor'}}),flush=True);continue
  if 'cursor' not in p:result={'data':[{'name':'expired','authStatus':'notLoggedIn','tools':{}}],'nextCursor':'next'}
  else:result={'data':[{'name':'working','authStatus':'oAuth','tools':{'read':{}}}],'nextCursor':None}
 elif method=='app/list':result={'data':[{'id':'connected','name':'Connected app','isEnabled':True,'isAccessible':True},{'id':'catalog-only','name':'Unused','isEnabled':True,'isAccessible':False}]}
 else:result={}
 print(json.dumps({'id':r['id'],'result':result}),flush=True)
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	checks := probeCodex(ctx, dir, time.Now())
	if len(checks) != 3 || checks[0].State != NeedsAuth || checks[0].Action == nil || checks[1].State != OK || checks[2].State != Unknown {
		t.Fatalf("incorrect inventory: %+v", checks)
	}
}
func TestClaudeAuthAndHandshakeScope(t *testing.T) {
	checks := parseClaudeMCP("plugin:a:one: https://secret.invalid - ✓ Connected\nexpired: https://token.invalid - ! Needs authentication\npending: command - ⏸ Pending approval", "workspace", time.Now())
	if len(checks) != 3 || checks[0].State != OK || checks[1].State != NeedsAuth || checks[2].State != Unknown {
		t.Fatalf("incorrect inventory: %+v", checks)
	}
	if checks[0].Name != "plugin:a:one" || checks[1].Action.Workspace != "workspace" {
		t.Fatal("workspace or name lost")
	}
}

func TestClaudeCatalogDoesNotBecomeLoginTasks(t *testing.T) {
	output := "claude.ai Linear: https://example.invalid - ! Needs authentication\n" +
		"claude.ai Microsoft 365: https://example.invalid - Needs authentication\n" +
		"claude.ai neon: https://example.invalid - Failed to connect\n" +
		"claude.ai Working: https://example.invalid - ✓ Connected\n" +
		"configured-server: https://example.invalid - Needs authentication"
	checks := parseClaudeMCP(output, "workspace", time.Now())
	if len(checks) != 3 {
		t.Fatalf("expected a live integration, configured MCP, and one coverage row: %+v", checks)
	}
	if checks[0].State != OK || checks[1].State != NeedsAuth || checks[1].Action == nil {
		t.Fatal("lost the handshake or configured MCP auth failure")
	}
	if checks[2].State != Unknown || checks[2].Action != nil {
		t.Fatal("cloud catalog became a sign-in task")
	}
	tasks := Tasks(Report{Checks: checks})
	if len(tasks) != 1 || tasks[0].Check.Name != "configured-server" {
		t.Fatalf("unused cloud connectors became tasks: %+v", tasks)
	}
}

func TestClaudeCloudHistoryCannotCreateSignInTasks(t *testing.T) {
	dir := t.TempDir()
	history := `{"claudeAiMcpEverConnected":["claude.ai Linear","claude.ai Microsoft 365","claude.ai neon"]}`
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(history), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	script := `#!/bin/sh
printf '%s\n' 'claude.ai Linear: https://example.invalid - Needs authentication' 'claude.ai Microsoft 365: https://example.invalid - Needs authentication' 'claude.ai neon: https://example.invalid - Needs authentication'
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	checks := probeClaude(ctx, dir, time.Now())
	if len(checks) != 1 || checks[0].State != Unknown || checks[0].Action != nil {
		t.Fatalf("historical connectors became current auth issues: %+v", checks)
	}
	if len(Tasks(Report{Checks: checks})) != 0 {
		t.Fatal("historical connections created repair tasks")
	}
}

func TestClaudeCloudFailureTextCannotCountAsConnected(t *testing.T) {
	for _, state := range []string{"disconnected", "not connected", "Needs authentication (previously connected)", "Failed to connect", "Disabled", "Rejected"} {
		t.Run(state, func(t *testing.T) {
			checks := parseClaudeMCP("claude.ai Unused: https://example.invalid - "+state, "workspace", time.Now())
			if len(checks) != 1 || checks[0].State != Unknown || checks[0].Action != nil || checks[0].Name != "Claude app connectors" {
				t.Fatalf("catalog failure classified as connected or actionable: %+v", checks)
			}
		})
	}
}

func TestCodexCredentialMechanismsDoNotOfferOAuth(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
	}{
		{"oauth", `{"transport":{"type":"streamable_http"}}`, "oauth"},
		{"bearer environment", `{"transport":{"type":"streamable_http","bearer_token_env_var":"TEST_MCP_TOKEN"}}`, "credential"},
		{"static header", `{"transport":{"type":"streamable_http","http_headers":{"Authorization":"test-only"}}}`, "credential"},
		{"environment header", `{"transport":{"type":"streamable_http","env_http_headers":{"X-API-Key":"TEST_MCP_KEY"}}}`, "credential"},
		{"header helper", `{"transport":{"type":"streamable_http","http_headers_helper":"test-only"}}`, "credential"},
		{"stdio", `{"transport":{"type":"stdio"}}`, ""},
		{"invalid", `{`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCodexAuthMechanism([]byte(tc.config)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodexBearerStatusCannotProduceOAuthAction(t *testing.T) {
	dir := t.TempDir()
	script := `#!/usr/bin/python3
import sys,json
if len(sys.argv)>1 and sys.argv[1]=='mcp':
 print(json.dumps({'transport':{'type':'streamable_http','bearer_token_env_var':'TEST_MCP_TOKEN'}}));sys.exit(0)
for line in sys.stdin:
 r=json.loads(line);method=r['method']
 if method=='initialized':continue
 if method=='mcpServerStatus/list':result={'data':[{'name':'token-server','authStatus':'notLoggedIn','tools':{}}]}
 elif method=='app/list':result={'data':[]}
 else:result={}
 print(json.dumps({'id':r['id'],'result':result}),flush=True)
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	checks := probeCodex(ctx, dir, time.Now())
	if len(checks) != 2 || checks[0].State != Unknown || checks[0].Action != nil {
		t.Fatalf("bearer-token server became an OAuth login task: %+v", checks)
	}
}
func TestHubRetainsResultsOnDeadline(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ai/tool/scripts")
	os.MkdirAll(dir, 0700)
	script := `import types,time
probes=types.SimpleNamespace()
def load_config():return {}
def repo_profile(name):return {}
def derive_manifest(repos,profile,stage):return [{'id':'cognito:pennyworth'},{'id':'slow'}]
def make_prober(cfg):
 def probe(name):
  if name=='slow':time.sleep(10)
  return {'status':'expired'}
 return probe
`
	os.WriteFile(filepath.Join(dir, "preflight.py"), []byte(script), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	checks := probeHub(ctx, Config{HubCheckout: root, HubRepos: []string{"test"}}, time.Now())
	if len(checks) != 2 || checks[0].State != NeedsAuth || checks[1].State != Unknown {
		t.Fatalf("completed result lost: %+v", checks)
	}
}
