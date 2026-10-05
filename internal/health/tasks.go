package health

import (
	"sort"
	"strings"
)

// Task is one repair, which can affect checks in several workspaces.
type Task struct {
	Key    string
	Check  Check
	Scopes []string
	Count  int
}

// Tasks groups confirmed problems by repair. Unknown checks are diagnostics,
// not tasks. A missing repair is still shown as a problem for manual review.
func Tasks(report Report) []Task {
	checks := append([]Check(nil), report.Checks...)
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].ID < checks[j].ID })
	positions := map[string]int{}
	result := []Task{}
	for _, check := range checks {
		if !check.NeedsAttention() {
			continue
		}
		key := taskKey(check)
		i, exists := positions[key]
		if !exists {
			i = len(result)
			positions[key] = i
			result = append(result, Task{Key: key, Check: check})
		}
		task := &result[i]
		task.Count++
		if check.Scope != "" && !containsScope(task.Scopes, check.Scope) {
			task.Scopes = append(task.Scopes, check.Scope)
		}
		if check.State == NeedsAuth {
			task.Check.State = NeedsAuth
		}
	}
	for i := range result {
		sort.Strings(result[i].Scopes)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := taskPriority(result[i]), taskPriority(result[j])
		if left != right {
			return left < right
		}
		return result[i].Key < result[j].Key
	})
	return result
}

func taskKey(check Check) string {
	if action := check.Action; action != nil {
		// Present duplicate host and repair targets as one action. The launch
		// still uses a monitored workspace and the host's own credential store.
		key := strings.Join([]string{"repair", action.Kind, action.Host, action.Target}, "\x00")
		// A browser storage file belongs to its maintained checkout.
		if action.Kind == "cognito-login" {
			key += "\x00" + action.Workspace
		}
		return key
	}
	return strings.Join([]string{"manual", check.Host, check.Name, string(check.State), check.Detail}, "\x00")
}

func containsScope(scopes []string, scope string) bool {
	for _, value := range scopes {
		if value == scope {
			return true
		}
	}
	return false
}

func taskPriority(task Task) int {
	if task.Check.Action == nil {
		return 2
	}
	if task.Check.State == NeedsAuth {
		return 0
	}
	return 1
}
