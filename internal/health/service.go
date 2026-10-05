package health

import (
	"context"
	"github.com/chuongtrh/ai-quota/internal/storage"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Service struct {
	Updates   chan struct{}
	mu        sync.Mutex
	config    Config
	dataDir   string
	report    Report
	lastCheck time.Time
	failures  map[string]int
	notified  map[string]State
	notify    func(string, string) error
	now       func() time.Time
}

func New(dataDir string, notify func(string, string) error) *Service {
	home, _ := os.UserHomeDir()
	s := &Service{Updates: make(chan struct{}, 1), dataDir: dataDir, config: Config{ReadinessURL: "http://localhost:3000/api/readiness", Workspaces: []string{home}, IntervalMinutes: 5}, failures: map[string]int{}, notified: map[string]State{}, notify: notify, now: time.Now}
	if err := storage.ReadJSON(filepath.Join(dataDir, "health-config.json"), &s.config); os.IsNotExist(err) {
		_ = storage.WriteJSON(filepath.Join(dataDir, "health-config.json"), s.config, 0o600)
	}
	if s.config.IntervalMinutes < 1 {
		s.config.IntervalMinutes = 5
	}
	if len(s.config.Workspaces) == 0 {
		s.config.Workspaces = []string{home}
	}
	if len(s.config.Workspaces) > 8 {
		s.config.Workspaces = s.config.Workspaces[:8]
	}
	_ = storage.ReadJSON(filepath.Join(dataDir, "health-notifications.json"), &s.notified)
	_ = storage.ReadJSON(filepath.Join(dataDir, "health-report.json"), &s.report)
	for i := range s.report.Checks {
		s.report.Checks[i].State = Unknown
		s.report.Checks[i].Detail = "Saved result; waiting for a fresh check"
		s.report.Checks[i].Action = nil
	}
	return s
}

func (s *Service) ConfigPath() string { return filepath.Join(s.dataDir, "health-config.json") }
func (s *Service) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.config
	c.Workspaces = append([]string(nil), c.Workspaces...)
	return c
}
func (s *Service) Snapshot() Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.report
	r.Checks = append([]Check(nil), r.Checks...)
	for i := range r.Checks {
		if s.now().Sub(r.Checks[i].CheckedAt) > time.Duration(s.config.IntervalMinutes*2+1)*time.Minute && r.Checks[i].State != Disabled {
			r.Checks[i].State = Unknown
			r.Checks[i].Detail = "Result is stale; choose Check now"
			r.Checks[i].Action = nil
		}
	}
	return r
}
func (s *Service) Due() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.report.Checking && s.now().Sub(s.lastCheck) >= time.Duration(s.config.IntervalMinutes)*time.Minute
}
func (s *Service) AddWorkspace(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.config.Workspaces {
		if p == path {
			return nil
		}
	}
	if len(s.config.Workspaces) >= 8 {
		return os.ErrInvalid
	}
	s.config.Workspaces = append(s.config.Workspaces, path)
	return storage.WriteJSON(filepath.Join(s.dataDir, "health-config.json"), s.config, 0o600)
}

func (s *Service) Check(ctx context.Context) {
	s.mu.Lock()
	if s.report.Checking {
		s.mu.Unlock()
		return
	}
	var updated Config
	if storage.ReadJSON(s.ConfigPath(), &updated) == nil && updated.IntervalMinutes >= 1 && len(updated.Workspaces) <= 8 {
		s.config = updated
	}
	s.report.Checking = true
	s.mu.Unlock()
	config := s.Config()
	now := s.now()
	type task func(context.Context) []Check
	tasks := []task{}
	if config.ReadinessURL != "" {
		tasks = append(tasks, func(c context.Context) []Check { return probeReadiness(c, config.ReadinessURL, now) })
	}
	if config.HubCheckout != "" && len(config.HubRepos) > 0 {
		tasks = append(tasks, func(c context.Context) []Check { return probeHub(c, config, now) })
	}
	for _, workspace := range config.Workspaces {
		tasks = append(tasks, func(c context.Context) []Check { return probeClaude(c, workspace, now) }, func(c context.Context) []Check { return probeCodex(c, workspace, now) })
	}
	results := make(chan []Check, len(tasks))
	slots := make(chan struct{}, 3)
	for _, probe := range tasks {
		go func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				results <- nil
				return
			}
			defer func() { <-slots }()
			c, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			results <- probe(c)
		}()
	}
	checks := []Check{}
	for range tasks {
		checks = append(checks, (<-results)...)
	}
	if ctx.Err() != nil {
		s.mu.Lock()
		s.report.Checking = false
		s.mu.Unlock()
		return
	}
	attachSourceActions(checks, sourceSelectors())
	checks = uniqueChecks(checks)
	sort.Slice(checks, func(i, j int) bool { return checks[i].ID < checks[j].ID })
	s.publish(checks, now)
}

func (s *Service) publish(checks []Check, now time.Time) {
	s.mu.Lock()
	notices := []Check{}
	pending := false
	for i, c := range checks {
		if c.State == Failed {
			s.failures[c.ID]++
			if s.failures[c.ID] < 2 {
				checks[i].State = Unknown
				checks[i].Detail = "First check failed; confirming before alerting. " + c.Detail
				pending = true
				continue
			}
		} else {
			s.failures[c.ID] = 0
		}
		if c.NeedsAttention() && s.notified[c.ID] != c.State {
			notices = append(notices, c)
		}
		// Unknown results do not re-arm an alert or claim recovery.
		if c.State != Unknown {
			s.notified[c.ID] = c.State
		}
	}
	s.report = Report{CheckedAt: now, Checks: checks}
	s.lastCheck = now
	if pending {
		s.lastCheck = now.Add(-time.Duration(s.config.IntervalMinutes)*time.Minute + 30*time.Second)
	}
	report := s.report
	states := make(map[string]State, len(s.notified))
	for k, v := range s.notified {
		states[k] = v
	}
	s.mu.Unlock()
	select {
	case s.Updates <- struct{}{}:
	default:
	}
	_ = storage.WriteJSON(filepath.Join(s.dataDir, "health-report.json"), report, 0o600)
	_ = storage.WriteJSON(filepath.Join(s.dataDir, "health-notifications.json"), states, 0o600)
	if s.notify != nil && len(notices) > 0 {
		_ = s.notify("AI tools need attention", notices[0].Host+": "+notices[0].Name+". Open AIQuota for details.")
	}
}

func (s *Service) ConsumeAuthCompletion() bool {
	path := filepath.Join(s.dataDir, "auth-completed")
	if _, err := os.Stat(path); err != nil {
		return false
	}
	_ = os.Remove(path)
	return true
}
func (s *Service) RunAction(ctx context.Context, a Action) error {
	return LaunchAction(ctx, a, s.dataDir)
}
