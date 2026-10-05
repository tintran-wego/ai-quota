package desktop

type Button struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Disabled bool   `json:"disabled,omitempty"`
}

type Task struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Scope  string `json:"scope,omitempty"`
	Action Button `json:"action"`
}

// View keeps repairs separate from optional diagnostic detail.
type View struct {
	Summary string   `json:"summary"`
	Quota   string   `json:"quota"`
	Tasks   []Task   `json:"tasks"`
	Details string   `json:"details"`
	Buttons []Button `json:"buttons"`
}
