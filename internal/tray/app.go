package tray

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fyne.io/systray"

	"github.com/chuongtrh/ai-quota/internal/appcore"
	"github.com/chuongtrh/ai-quota/internal/desktop"
	"github.com/chuongtrh/ai-quota/internal/health"
	appicon "github.com/chuongtrh/ai-quota/internal/icon"
	"github.com/chuongtrh/ai-quota/internal/model"
	"github.com/chuongtrh/ai-quota/internal/notify"
	"github.com/chuongtrh/ai-quota/internal/provider/antigravity"
	"github.com/chuongtrh/ai-quota/internal/provider/claude"
	"github.com/chuongtrh/ai-quota/internal/provider/codex"
)

type App struct {
	health                *health.Service
	actions               map[int]health.Action
	healthSummary         *systray.MenuItem
	checkHealth           *systray.MenuItem
	details               *systray.MenuItem
	service               *appcore.Service
	codexInstaller        codex.Installer
	claudeInstaller       claude.Installer
	antigravityInstaller  antigravity.Installer
	codexConnection       connectionStateCache
	claudeConnection      connectionStateCache
	antigravityConnection connectionStateCache
	version               string
	ctx                   context.Context
	cancel                context.CancelFunc

	quotaItems    map[model.Provider]*providerQuotaItems
	providerItems map[model.Provider]providerControlItems
	waiting       *systray.MenuItem
	updated       *systray.MenuItem
	refresh       *systray.MenuItem
	quit          *systray.MenuItem
}

type connectionStateCache struct {
	check       func() (bool, error)
	interval    time.Duration
	now         func() time.Time
	checkedAt   time.Time
	initialized bool
	connected   bool
	err         error
}

func (c *connectionStateCache) Get(force bool) (bool, error) {
	now := c.now()
	if !force && c.initialized && now.Sub(c.checkedAt) < c.interval {
		return c.connected, c.err
	}
	c.connected, c.err = c.check()
	c.checkedAt = now
	c.initialized = true
	return c.connected, c.err
}

const maxQuotaRowsPerProvider = 8

type quotaRow struct {
	text     string
	severity model.Severity
	active   bool
}

type providerQuotaItems struct {
	header *systray.MenuItem
	rows   []*systray.MenuItem
}

func (items *providerQuotaItems) Show() {
	items.header.Show()
}

func (items *providerQuotaItems) Hide() {
	items.header.Hide()
	for _, row := range items.rows {
		row.Hide()
	}
}

func (items *providerQuotaItems) SetRows(rows []quotaRow) {
	for index, row := range items.rows {
		if index < len(rows) {
			r := rows[index]
			row.SetTitle(r.text)
			row.SetIcon(appicon.StatusDotPNG(32, r.severity, r.active))
			row.Show()
		} else {
			row.Hide()
		}
	}
}

type providerControlItems struct {
	status *systray.MenuItem
	action *systray.MenuItem
}

var providerOrder = []model.Provider{
	model.ProviderCodex,
	model.ProviderClaudeCode,
	model.ProviderAntigravity,
}

func New(
	service *appcore.Service,
	healthService *health.Service,
	codexInstaller codex.Installer,
	claudeInstaller claude.Installer,
	antigravityInstaller antigravity.Installer,
	version string,
) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{
		service:              service,
		health:               healthService,
		actions:              map[int]health.Action{},
		codexInstaller:       codexInstaller,
		claudeInstaller:      claudeInstaller,
		antigravityInstaller: antigravityInstaller,
		codexConnection: connectionStateCache{
			check:    codexInstaller.IsConnected,
			interval: time.Minute,
			now:      time.Now,
		},
		claudeConnection: connectionStateCache{
			check:    claudeInstaller.IsConnected,
			interval: time.Minute,
			now:      time.Now,
		},
		antigravityConnection: connectionStateCache{
			check:    antigravityInstaller.IsConnected,
			interval: time.Minute,
			now:      time.Now,
		},
		version:       version,
		ctx:           ctx,
		cancel:        cancel,
		quotaItems:    make(map[model.Provider]*providerQuotaItems),
		providerItems: make(map[model.Provider]providerControlItems),
	}
}

func (a *App) Run() {
	systray.Run(a.onReady, a.onExit)
}

func (a *App) onReady() {
	trayIcon := appicon.TrayPNG(36)
	systray.SetTemplateIcon(trayIcon, trayIcon)
	systray.SetTitle("—")
	systray.SetTooltip("AI quota")
	systray.SetRemovalAllowed(false)

	for _, provider := range providerOrder {
		header := disabledItem(provider.DisplayName())
		items := &providerQuotaItems{
			header: header,
		}
		for i := 0; i < maxQuotaRowsPerProvider; i++ {
			row := systray.AddMenuItem("—", "")
			row.Hide()
			items.rows = append(items.rows, row)
		}
		items.Hide()
		a.quotaItems[provider] = items
	}
	desktop.Configure()
	a.waiting = disabledItem("⏳ Waiting for provider setup")
	systray.AddSeparator()
	a.healthSummary = disabledItem("Connections: waiting for checks")
	a.checkHealth = systray.AddMenuItem("Check connections now", "Run read-only health probes")
	a.details = systray.AddMenuItem("Open readiness window", "View connection issues and sign-in actions")
	disabledItem("🔔 Alerts at 20% and 5% remaining")
	a.updated = disabledItem("🔄 Not updated yet")
	a.refresh = systray.AddMenuItem("↻ Refresh now", "Fetch the latest quota data")
	providers := systray.AddMenuItem("🧩 Providers", "")
	for _, provider := range providerOrder {
		providerMenu := providers.AddSubMenuItem(provider.DisplayName(), "")
		status := providerMenu.AddSubMenuItem("⏳ Checking status", "")
		status.Disable()
		action := providerMenu.AddSubMenuItem(initialProviderAction(provider), initialProviderActionTooltip(provider))
		a.providerItems[provider] = providerControlItems{status: status, action: action}
	}
	systray.AddSeparator()
	disabledItem("ℹ️ Version " + a.version)
	a.quit = systray.AddMenuItem("⏻ Quit", "Quit AI quota")

	for _, items := range a.quotaItems {
		for _, row := range items.rows {
			go func(ch chan struct{}) {
				for {
					select {
					case <-a.ctx.Done():
						return
					case <-ch:
						go a.service.Refresh(a.ctx)
					}
				}
			}(row.ClickedCh)
		}
	}

	notify.RequestPermission()
	go a.service.Refresh(a.ctx)
	go a.health.Check(a.ctx)
	a.updateMenu()
	go a.eventLoop()
}

func (a *App) eventLoop() {
	refreshTicker := time.NewTicker(60 * time.Second)
	uiTicker := time.NewTicker(30 * time.Second)
	defer refreshTicker.Stop()
	defer uiTicker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-refreshTicker.C:
			go a.service.Refresh(a.ctx)
		case <-uiTicker.C:
			if a.health.Due() || a.health.ConsumeAuthCompletion() {
				go a.health.Check(a.ctx)
			}
			a.updateMenu()
		case <-a.health.Updates:
			a.updateMenu()
		case <-a.checkHealth.ClickedCh:
			go a.health.Check(a.ctx)
		case <-a.details.ClickedCh:
			desktop.Show()
		case id := <-desktop.Events:
			a.nativeAction(id)
		case path := <-desktop.Workspaces:
			if err := a.health.AddWorkspace(path); err != nil {
				a.reportActionError(err)
			} else {
				go a.health.Check(a.ctx)
			}
		case <-a.refresh.ClickedCh:
			go a.service.Refresh(a.ctx)
		case <-a.providerItems[model.ProviderCodex].action.ClickedCh:
			a.toggleCodexTracking()
		case <-a.providerItems[model.ProviderClaudeCode].action.ClickedCh:
			a.toggleClaudeTracking()
		case <-a.providerItems[model.ProviderAntigravity].action.ClickedCh:
			a.toggleAntigravityTracking()
		case <-a.quit.ClickedCh:
			systray.Quit()
		}
	}
}

func (a *App) updateMenu() {
	statuses := a.service.Statuses()
	codexConnected, codexSettingsErr := a.codexConnection.Get(false)
	claudeConnected, claudeSettingsErr := a.claudeConnection.Get(false)
	antigravityConnected, antigravitySettingsErr := a.antigravityConnection.Get(false)
	visibleProviders := make(map[model.Provider]bool, len(providerOrder))
	visibleCount := 0
	for _, provider := range providerOrder {
		connected := true
		var settingsErr error
		switch provider {
		case model.ProviderCodex:
			connected, settingsErr = codexConnected, codexSettingsErr
		case model.ProviderClaudeCode:
			connected, settingsErr = claudeConnected, claudeSettingsErr
		case model.ProviderAntigravity:
			connected, settingsErr = antigravityConnected, antigravitySettingsErr
		}
		visible := providerVisible(provider, statuses[provider], connected, settingsErr)
		visibleProviders[provider] = visible
		if a.updateProvider(statuses[provider], a.quotaItems[provider], visible) {
			visibleCount++
		}
	}

	if visibleCount == 0 {
		if codexConnected && codexSettingsErr == nil || claudeConnected && claudeSettingsErr == nil || antigravityConnected && antigravitySettingsErr == nil {
			a.waiting.SetTitle("⏳ Waiting for quota data")
		} else {
			a.waiting.SetTitle("⏳ Waiting for provider setup")
		}
		a.waiting.Show()
	} else {
		a.waiting.Hide()
	}

	remaining, _, available := mostUrgentRemaining(statuses, visibleProviders, time.Now())
	report := a.health.Snapshot()
	severity := model.SeverityHealthy
	if available {
		severity = model.SeverityForRemaining(float64(remaining))
	}
	if report.Issues() > 0 {
		severity = model.SeverityCritical
	}
	systray.SetIcon(appicon.TrayColorPNG(36, severity, available || report.Issues() > 0))
	systray.SetTitle(badgeTitle(report, remaining, available))
	a.updateReadiness(statuses)

	var tooltipLines []string
	tooltipLines = append(tooltipLines, "AI quota")
	for _, provider := range providerOrder {
		if !visibleProviders[provider] {
			continue
		}
		status := statuses[provider]
		minP := -1
		for _, w := range status.Windows {
			if w.ResetsAt.After(time.Now()) {
				r := w.RoundedRemainingPercent()
				if minP == -1 || r < minP {
					minP = r
				}
			}
		}
		if minP >= 0 {
			tooltipLines = append(tooltipLines, fmt.Sprintf("%s: %d%%", provider.DisplayName(), minP))
		}
	}
	systray.SetTooltip(strings.Join(tooltipLines, "\n"))

	latest := time.Time{}
	for provider, status := range statuses {
		if !visibleProviders[provider] {
			continue
		}
		if status.UpdatedAt.After(latest) {
			latest = status.UpdatedAt
		}
	}
	if latest.IsZero() {
		a.updated.SetTitle("🔄 No data yet")
	} else {
		a.updated.SetTitle("🔄 Updated " + formatAgo(time.Since(latest)))
	}

	a.updateStatusLineProviderMenu(model.ProviderCodex, statuses[model.ProviderCodex], codexConnected, codexSettingsErr)
	a.updateStatusLineProviderMenu(model.ProviderClaudeCode, statuses[model.ProviderClaudeCode], claudeConnected, claudeSettingsErr)
	a.updateStatusLineProviderMenu(model.ProviderAntigravity, statuses[model.ProviderAntigravity], antigravityConnected, antigravitySettingsErr)
}

func (a *App) updateProvider(
	status model.ProviderStatus,
	items *providerQuotaItems,
	visible bool,
) bool {
	if !visible {
		items.Hide()
		return false
	}
	items.Show()
	headerTitle := status.Provider.DisplayName()
	if headerTitle == "" {
		headerTitle = "—"
	}
	if status.Error != "" && !isWaitingForData(status) {
		headerTitle = "⚠️ " + headerTitle
	}
	items.header.SetTitle(headerTitle)
	items.SetRows(quotaRowsForProvider(status, time.Now()))
	return true
}

func (a *App) toggleCodexTracking() {
	connected, err := a.codexConnection.Get(true)
	if err != nil {
		_ = notify.Send("⚠️ Could not read Codex settings", err.Error())
		return
	}
	if connected {
		err = a.codexInstaller.Disconnect()
		if err == nil {
			a.service.ClearProvider(model.ProviderCodex)
			_ = notify.Send("AI quota", "Codex tracking was disabled.")
		}
	} else {
		err = a.codexInstaller.Connect()
		if err == nil {
			a.service.ClearProvider(model.ProviderCodex)
			go a.service.Refresh(a.ctx)
			_ = notify.Send("AI quota", "Codex tracking is enabled.")
		}
	}
	if err != nil {
		_ = notify.Send("⚠️ Could not update Codex tracking", err.Error())
	}
	_, _ = a.codexConnection.Get(true)
	a.updateMenu()
}

func (a *App) toggleClaudeTracking() {
	connected, err := a.claudeConnection.Get(true)
	if err != nil {
		_ = notify.Send("⚠️ Could not read Claude Code settings", err.Error())
		return
	}
	if connected {
		err = a.claudeInstaller.Disconnect()
		if err == nil {
			a.service.ClearProvider(model.ProviderClaudeCode)
			_ = notify.Send("AI quota", "Claude Code tracking was disabled and its previous status line was restored.")
		}
	} else {
		err = a.claudeInstaller.Connect()
		if err == nil {
			a.service.ClearProvider(model.ProviderClaudeCode)
			go a.service.Refresh(a.ctx)
			_ = notify.Send("AI quota", "Claude Code tracking is enabled. Send a prompt to receive the latest quota data.")
		}
	}
	if err != nil {
		message := err.Error()
		if errors.Is(err, claude.ErrSettingsChanged) {
			message = "The status line changed after tracking was enabled. AI quota will not overwrite the current settings."
		}
		_ = notify.Send("⚠️ Could not update Claude Code tracking", message)
	}
	_, _ = a.claudeConnection.Get(true)
	a.updateMenu()
}

func (a *App) toggleAntigravityTracking() {
	connected, err := a.antigravityConnection.Get(true)
	if err != nil {
		_ = notify.Send("⚠️ Could not read Google Antigravity CLI settings", err.Error())
		return
	}
	if connected {
		err = a.antigravityInstaller.Disconnect()
		if err == nil {
			a.service.ClearProvider(model.ProviderAntigravity)
			_ = notify.Send("AI quota", "Google Antigravity CLI tracking was disabled and its previous status line was restored.")
		}
	} else {
		err = a.antigravityInstaller.Connect()
		if err == nil {
			a.service.ClearProvider(model.ProviderAntigravity)
			go a.service.Refresh(a.ctx)
			_ = notify.Send("AI quota", "Google Antigravity tracking is enabled.")
		}
	}
	if err != nil {
		message := err.Error()
		if errors.Is(err, antigravity.ErrSettingsChanged) {
			message = "The status line changed after tracking was enabled. AI quota will not overwrite the current settings."
		}
		_ = notify.Send("⚠️ Could not update Google Antigravity CLI tracking", message)
	}
	_, _ = a.antigravityConnection.Get(true)
	a.updateMenu()
}

func (a *App) updateStatusLineProviderMenu(provider model.Provider, status model.ProviderStatus, connected bool, settingsErr error) {
	items := a.providerItems[provider]
	state := statusLineProviderMenuState(provider, status, connected, settingsErr)
	items.status.SetTitle(state.statusTitle)
	items.status.SetTooltip(state.statusTooltip)
	items.action.SetTitle(state.actionTitle)
	items.action.SetTooltip(state.actionTooltip)
}

type providerMenuState struct {
	statusTitle   string
	statusTooltip string
	actionTitle   string
	actionTooltip string
}

func statusLineProviderMenuState(provider model.Provider, status model.ProviderStatus, connected bool, settingsErr error) providerMenuState {
	connector := "Enable quota tracking for " + provider.DisplayName()
	if settingsErr != nil {
		return providerMenuState{
			statusTitle: "⚠️ Settings unavailable", statusTooltip: settingsErr.Error(),
			actionTitle: "Enable tracking", actionTooltip: connector,
		}
	}
	if !connected {
		return providerMenuState{
			statusTitle: "⚪ Tracking disabled", statusTooltip: provider.DisplayName() + " quota tracking is disabled",
			actionTitle: "Enable tracking", actionTooltip: connector,
		}
	}
	state := providerMenuState{actionTitle: "Disable tracking", actionTooltip: "Disable quota tracking for " + provider.DisplayName()}
	switch {
	case len(status.Windows) == 0:
		state.statusTitle = "⏳ Waiting for quota data"
		state.statusTooltip = "Send a " + provider.DisplayName() + " prompt to receive quota data"
	case status.Error != "":
		state.statusTitle = "🟡 Showing cached quota"
		state.statusTooltip = status.Error
	default:
		state.statusTitle = "🟢 Tracking active"
		state.statusTooltip = provider.DisplayName() + " quota data is available"
	}
	return state
}

func initialProviderAction(provider model.Provider) string {
	return "Enable tracking"
}

func initialProviderActionTooltip(provider model.Provider) string {
	return "Enable quota tracking for " + provider.DisplayName()
}

func mostUrgentRemaining(
	statuses map[model.Provider]model.ProviderStatus,
	visible map[model.Provider]bool,
	now time.Time,
) (int, model.Provider, bool) {
	minimum := 101
	var urgentProvider model.Provider
	found := false
	for _, provider := range providerOrder {
		if !visible[provider] {
			continue
		}
		status := statuses[provider]
		for _, window := range status.Windows {
			if !window.ResetsAt.After(now) {
				continue
			}
			remaining := window.RoundedRemainingPercent()
			if remaining < minimum {
				minimum = remaining
				urgentProvider = provider
				found = true
			}
		}
	}
	return minimum, urgentProvider, found
}

func providerVisible(provider model.Provider, status model.ProviderStatus, connected bool, settingsErr error) bool {
	return connected && settingsErr == nil
}

func (a *App) onExit() {
	a.cancel()
	a.health.WaitActions()
}

func disabledItem(title string) *systray.MenuItem {
	item := systray.AddMenuItem(title, "")
	item.Disable()
	return item
}

func tabPaddingForName(name string) string {
	switch {
	case strings.HasPrefix(name, "Claude") || len(name) >= 15:
		return "\t"
	case strings.HasPrefix(name, "Gemini") || len(name) >= 9:
		return "\t\t"
	default:
		return "\t\t\t\t"
	}
}

func formatPercentText(percent int) string {
	if percent < 100 {
		return fmt.Sprintf("\u2007%d%%", percent)
	}
	return fmt.Sprintf("%d%%", percent)
}

func formatWindow(status model.ProviderStatus, kind model.WindowKind, now time.Time) quotaRow {
	name := kind.DisplayName()
	tabs := tabPaddingForName(name)
	window, exists := status.Window(kind)
	if !exists {
		return quotaRow{
			text:     fmt.Sprintf("%s%s—", name, tabs),
			severity: model.SeverityHealthy,
			active:   false,
		}
	}
	return formatQuotaWindow(window, now)
}

func windowRows(status model.ProviderStatus, now time.Time) []quotaRow {
	rows := make([]quotaRow, 0, len(status.Windows))
	for _, window := range status.Windows {
		rows = append(rows, formatQuotaWindow(window, now))
	}
	return rows
}

func isWaitingForData(status model.ProviderStatus) bool {
	if len(status.Windows) > 0 {
		return false
	}
	if status.Error == "" {
		return true
	}
	errStr := strings.ToLower(status.Error)
	return strings.Contains(errStr, "no active quota data") ||
		strings.Contains(errStr, "tracking is disabled") ||
		strings.Contains(errStr, "waiting for") ||
		strings.Contains(errStr, "not sent quota data yet")
}

func quotaRowsForProvider(status model.ProviderStatus, now time.Time) []quotaRow {
	if len(status.Windows) == 0 {
		if status.Error != "" && !isWaitingForData(status) {
			return []quotaRow{{text: status.Error, severity: model.SeverityCritical, active: true}}
		}
		return []quotaRow{{text: "Waiting for quota data", severity: model.SeverityHealthy, active: false}}
	}
	if status.Provider == model.ProviderAntigravity {
		return windowRows(status, now)
	}
	return []quotaRow{
		formatWindow(status, model.WindowSession, now),
		formatWindow(status, model.WindowWeekly, now),
	}
}

func formatQuotaWindow(window model.Window, now time.Time) quotaRow {
	name := window.DisplayName()
	tabs := tabPaddingForName(name)
	if !window.ResetsAt.After(now) {
		return quotaRow{
			text:     fmt.Sprintf("%s%swaiting for new data", name, tabs),
			severity: model.SeverityHealthy,
			active:   false,
		}
	}
	remaining := window.RemainingPercent()
	severity := model.SeverityForRemaining(remaining)
	return quotaRow{
		text: fmt.Sprintf(
			"%s%s%s left · %s",
			name,
			tabs,
			formatPercentText(window.RoundedRemainingPercent()),
			model.FormatReset(now, window.ResetsAt),
		),
		severity: severity,
		active:   true,
	}
}

func formatAgo(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		return fmt.Sprintf("%d min ago", int(duration/time.Minute))
	case duration < 24*time.Hour:
		return fmt.Sprintf("%d hr ago", int(duration/time.Hour))
	default:
		return fmt.Sprintf("%d days ago", int(duration/(24*time.Hour)))
	}
}

func (a *App) reportActionError(err error) {
	_ = notify.Send("AIQuota action failed", err.Error())
}
