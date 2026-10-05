package health

import "testing"

func TestTasksGroupOneLoginAcrossWorkspaces(t *testing.T) {
	checks := []Check{
		{ID: "claude/project-b/slack", Host: "Claude Code", Name: "slack", Scope: "/project-b", State: NeedsAuth, Action: &Action{Kind: "mcp-login", Host: "claude", Target: "slack", Workspace: "/project-b"}},
		{ID: "claude/project-a/slack", Host: "Claude Code", Name: "slack", Scope: "/project-a", State: NeedsAuth, Action: &Action{Kind: "mcp-login", Host: "claude", Target: "slack", Workspace: "/project-a"}},
		{ID: "codex/project-a/slack", Host: "Codex", Name: "slack", Scope: "/project-a", State: NeedsAuth, Action: &Action{Kind: "mcp-login", Host: "codex", Target: "slack", Workspace: "/project-a"}},
		{ID: "claude/project-c/slack", Host: "Claude Code", Name: "slack", Scope: "/project-c", State: OK},
		{ID: "codex/apps", Host: "Codex", Name: "Apps", State: Unknown},
	}
	tasks := Tasks(Report{Checks: checks})
	if len(tasks) != 2 {
		t.Fatalf("want two host-specific logins, got %+v", tasks)
	}
	if tasks[0].Count != 2 || len(tasks[0].Scopes) != 2 || tasks[0].Check.Action.Workspace != "/project-a" {
		t.Fatalf("Claude login must combine both scopes with a stable launch workspace: %+v", tasks[0])
	}
}

func TestTasksKeepDistinctIdentitiesAndUnrepairableProblems(t *testing.T) {
	tasks := Tasks(Report{Checks: []Check{
		{ID: "prod/aws", Name: "AWS session", State: NeedsAuth, Action: &Action{Kind: "aws-login", Target: "production"}},
		{ID: "staging/aws", Name: "AWS session", State: NeedsAuth, Action: &Action{Kind: "aws-login", Target: "staging"}},
		{ID: "prod/redis", Host: "fs-log-data", Name: "redis", Scope: "production", State: Failed, Detail: "Unreachable"},
		{ID: "staging/redis", Host: "fs-log-data", Name: "redis", Scope: "staging", State: Failed, Detail: "Unreachable"},
		{ID: "disabled", State: Disabled},
	}})
	if len(tasks) != 3 {
		t.Fatalf("want two separate AWS identities and one grouped manual Redis repair, got %+v", tasks)
	}
	if tasks[2].Check.Action != nil || tasks[2].Count != 2 || len(tasks[2].Scopes) != 2 {
		t.Fatalf("manual problems must stay visible: %+v", tasks[2])
	}
}

func TestCognitoStorageFilesRemainCheckoutScoped(t *testing.T) {
	tasks := Tasks(Report{Checks: []Check{
		{ID: "a", State: NeedsAuth, Action: &Action{Kind: "cognito-login", Workspace: "/checkout-a"}},
		{ID: "b", State: NeedsAuth, Action: &Action{Kind: "cognito-login", Workspace: "/checkout-b"}},
	}})
	if len(tasks) != 2 {
		t.Fatal("independent storage files need separate login actions")
	}
}
