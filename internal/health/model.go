package health

import "time"

type State string

const (
	OK        State = "ok"
	Failed    State = "failed"
	NeedsAuth State = "needs-auth"
	Unknown   State = "unknown"
	Disabled  State = "disabled"
)

type Action struct {
	Kind      string `json:"kind"`
	Host      string `json:"host,omitempty"`
	Target    string `json:"target,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type Check struct {
	ID        string    `json:"id"`
	Host      string    `json:"host"`
	Scope     string    `json:"scope"`
	Name      string    `json:"name"`
	State     State     `json:"state"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checked_at"`
	Action    *Action   `json:"action,omitempty"`
}

func (c Check) NeedsAttention() bool { return c.State == Failed || c.State == NeedsAuth }

type Report struct {
	CheckedAt time.Time `json:"checked_at"`
	Checks    []Check   `json:"checks"`
	Checking  bool      `json:"checking"`
}

func (r Report) Issues() int {
	n := 0
	for _, c := range r.Checks {
		if c.NeedsAttention() {
			n++
		}
	}
	return n
}

func (r Report) Unknowns() int {
	n := 0
	for _, c := range r.Checks {
		if c.State == Unknown {
			n++
		}
	}
	return n
}

type Config struct {
	HubCheckout     string   `json:"hub_checkout,omitempty"`
	HubRepos        []string `json:"hub_repos,omitempty"`
	ReadinessURL    string   `json:"readiness_url"`
	Workspaces      []string `json:"workspaces"`
	IntervalMinutes int      `json:"interval_minutes"`
}
