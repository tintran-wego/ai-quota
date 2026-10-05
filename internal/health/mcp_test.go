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
