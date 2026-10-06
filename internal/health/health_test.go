package health

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
)

func TestReadinessExpiredAndStale(t *testing.T) {
	now := time.Now().UTC()
	stamp := now
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("force") != "1" {
			t.Error("missing force refresh")
		}
		fmt.Fprintf(w, `{"generatedAt":%q,"stores":[{"store":"athena","environment":"production","state":"failed","detail":"Token expired SECRET"},{"store":"glue","environment":"production","state":"failed","detail":"Token expired"}]}`, stamp.Format(time.RFC3339))
	}))
	defer server.Close()
	checks := probeReadiness(context.Background(), server.URL, now)
	if len(checks) != 1 || checks[0].State != NeedsAuth || checks[0].Action != nil {
		t.Fatalf("must dedupe auth and never guess profile: %+v", checks)
	}
	attachSourceActions(checks, map[string]string{"FS_MCP_AWS_PRODUCTION_CREDENTIALS": "profile:explicit"})
	if checks[0].Action == nil || checks[0].Action.Target != "explicit" {
		t.Fatal("explicit profile action missing")
	}
	stamp = now.Add(-time.Hour)
	checks = probeReadiness(context.Background(), server.URL, now)
	if checks[0].State != Unknown {
		t.Fatal("stale result must be unknown")
	}
}
func TestKnownBadAlertsAndRecovery(t *testing.T) {
	notices := 0
	s := New(t.TempDir(), func(string, string) error { notices++; return nil })
	now := time.Now()
	bad := Check{ID: "broken", Host: "test", State: Failed, CheckedAt: now}
	s.publish([]Check{bad}, now)
	if s.Snapshot().Issues() != 0 || notices != 0 {
		t.Fatal("first transient must not alert")
	}
	s.publish([]Check{bad}, now)
	if s.Snapshot().Issues() != 1 || notices != 1 {
		t.Fatal("known bad must fire")
	}
	s.publish([]Check{bad}, now)
	if notices != 1 {
		t.Fatal("duplicate alert")
	}
	good := bad
	good.State = OK
	s.publish([]Check{good}, now)
	if s.Snapshot().Issues() != 0 {
		t.Fatal("badge must clear after recovery")
	}
	s.publish([]Check{bad}, now)
	s.publish([]Check{bad}, now)
	if notices != 2 {
		t.Fatal("new failure must rearm")
	}
}
func TestHubRegistrationIsNotAuthProof(t *testing.T) {
	checks, err := parseHub([]byte(`{"checks":[{"id":"slack-mcp","status":"ok"},{"id":"cognito:pennyworth","status":"expired"},{"id":"docker","status":"unreachable"}]}`), "/tmp/hub", time.Now())
	if err != nil || checks[0].State != Unknown || checks[1].State != NeedsAuth || checks[1].Action == nil || checks[2].State != Failed {
		t.Fatalf("incorrect mapping: %+v %v", checks, err)
	}
}
func TestShellQuoteAndTarget(t *testing.T) {
	value := "path with 'quote' $(touch /tmp/never-aiquota)"
	out, err := exec.Command("/bin/sh", "-c", "printf %s "+shellQuote(value)).Output()
	if err != nil || string(out) != value {
		t.Fatalf("unsafe quoting: %q %v", out, err)
	}
	for _, target := range []string{"--help", "a;echo bad", "$(id)", ""} {
		if validTarget(target) {
			t.Fatalf("accepted %q", target)
		}
	}
}

func TestExpiredAuthAlertsImmediately(t *testing.T) {
	notices := 0
	s := New(t.TempDir(), func(string, string) error { notices++; return nil })
	now := time.Now()
	s.publish([]Check{{ID: "expired", State: NeedsAuth, CheckedAt: now}}, now)
	if notices != 1 || s.Snapshot().Issues() != 1 {
		t.Fatal("expired credential must alert immediately")
	}
}

func TestJenkinsNetworkFailureUsesItsFailedTunnelRepair(t *testing.T) {
	checks, err := parseHub([]byte(`{"checks":[{"id":"jenkins:prod","status":"unreachable"},{"id":"tunnel:prod","status":"unreachable"},{"id":"jenkins:staging","status":"unreachable"},{"id":"tunnel:staging","status":"unreachable"}]}`), "/hub", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	attachJenkinsTunnelActions(checks)
	for _, c := range checks {
		if c.Action == nil || c.Action.Kind != "tunnel" {
			t.Fatalf("network failure became an auth task instead of its tunnel repair: %+v", c)
		}
		want := "production"
		if c.Name == "jenkins:staging" || c.Name == "tunnel:staging" {
			want = "staging"
		}
		if c.Action.Target != want {
			t.Fatalf("wrong environment for the tunnel repair: %+v", c)
		}
	}
	if tasks := Tasks(Report{Checks: checks}); len(tasks) != 2 {
		t.Fatalf("Jenkins and its tunnel must share one repair per environment: %+v", tasks)
	}
}

func TestJenkinsDoesNotOfferTunnelForTokenOrUnverifiedNetworkFailure(t *testing.T) {
	for _, tc := range []struct{ status, tunnel string }{
		{"missing", "unreachable"}, {"expired", "unreachable"}, {"unreachable", "ok"}, {"unreachable", "unknown"},
	} {
		t.Run(tc.status+"/"+tc.tunnel, func(t *testing.T) {
			data := fmt.Sprintf(`{"checks":[{"id":"jenkins:prod","status":%q},{"id":"tunnel:prod","status":%q}]}`, tc.status, tc.tunnel)
			checks, err := parseHub([]byte(data), "/hub", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			attachJenkinsTunnelActions(checks)
			if checks[0].Action != nil {
				t.Fatalf("unproven tunnel repair or OAuth sign-in offered: %+v", checks[0])
			}
		})
	}
}
