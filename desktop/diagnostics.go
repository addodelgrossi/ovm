package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/buildinfo"
)

const (
	// maxEventFeeds bounds concurrent event subscriptions.
	maxEventFeeds = 4
	// maxFeedEvents is the per-feed history kept for polling.
	maxFeedEvents = 1000
	// maxStabilityJobs bounds concurrent stability tests.
	maxStabilityJobs = 2
	// eventRetryDelay paces resubscription after a failed pull.
	eventRetryDelay = 3 * time.Second
	// maxEventRetries stops a feed after this many consecutive failures.
	maxEventRetries = 5
)

// Background job states.
const (
	jobRunning   = "running"
	jobStopped   = "stopped"
	jobFailed    = "failed"
	jobCompleted = "completed"
)

// EventRecord is one received event with a feed-local identifier.
type EventRecord struct {
	ID         uint64 `json:"id"`
	ReceivedAt string `json:"received_at"`
	core.EventDTO
}

type eventFeed struct {
	deviceID string
	cancel   context.CancelFunc

	mu      sync.Mutex
	events  []EventRecord
	nextID  uint64
	state   string
	failure *BindingError
}

type stabilityJob struct {
	deviceID string
	cancel   context.CancelFunc

	mu      sync.Mutex
	cycles  []core.StabilityCycle
	report  *core.StabilityReport
	state   string
	failure *BindingError
}

// diagnosticsState holds the background diagnostics of the App and the last
// measured results that a report can include.
type diagnosticsState struct {
	mu        sync.Mutex
	feeds     map[string]*eventFeed
	jobs      map[string]*stabilityJob
	clock     map[string]core.ClockCheckResult
	stability map[string]core.StabilityReport
}

func newDiagnosticsState() *diagnosticsState {
	return &diagnosticsState{
		feeds: map[string]*eventFeed{}, jobs: map[string]*stabilityJob{},
		clock: map[string]core.ClockCheckResult{}, stability: map[string]core.StabilityReport{},
	}
}

func newJobID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func validJobID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// beginBackground registers long-running work with the shutdown drain. The
// returned context is cancelled when the App closes.
func (a *App) beginBackground() (Manager, context.Context, func(), *BindingError) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.manager == nil || a.rootCtx == nil {
		return nil, nil, nil, bindingUnavailable("app_closed", "the desktop application is closed")
	}
	if a.inflight == 0 {
		a.idle = make(chan struct{})
	}
	a.inflight++
	ctx, cancel := context.WithCancel(a.rootCtx)
	var once sync.Once
	finish := func() {
		once.Do(func() {
			cancel()
			a.mu.Lock()
			a.inflight--
			if a.inflight == 0 {
				close(a.idle)
			}
			a.mu.Unlock()
		})
	}
	return a.manager, ctx, finish, nil
}

// --- Events ---------------------------------------------------------------

type StartEventsRequest struct {
	DeviceID    string `json:"device_id"`
	TopicFilter string `json:"topic_filter"`
}

type EventFeedRequest struct {
	FeedID  string `json:"feed_id"`
	AfterID uint64 `json:"after_id"`
}

type EventFeedState struct {
	FeedID string        `json:"feed_id"`
	State  string        `json:"state"`
	Error  *BindingError `json:"error,omitempty"`
	Events []EventRecord `json:"events"`
}

type EventFeedResponse struct {
	Result *EventFeedState `json:"result,omitempty"`
	Error  *BindingError   `json:"error,omitempty"`
}

// StartEvents subscribes to a device's events and keeps pulling in the
// background until StopEvents or shutdown.
func (a *App) StartEvents(request StartEventsRequest) EventFeedResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return EventFeedResponse{Error: err}
	}
	if len(request.TopicFilter) > 256 {
		return EventFeedResponse{Error: bindingValidation("invalid_topic_filter", "the topic filter is not valid")}
	}
	diagnostics := a.diagnostics
	diagnostics.mu.Lock()
	running := 0
	for _, feed := range diagnostics.feeds {
		feed.mu.Lock()
		if feed.state == jobRunning {
			running++
		}
		feed.mu.Unlock()
	}
	diagnostics.mu.Unlock()
	if running >= maxEventFeeds {
		return EventFeedResponse{Error: bindingValidation("too_many_feeds", "too many event feeds are running")}
	}

	manager, ctx, finish, bindingErr := a.beginBackground()
	if bindingErr != nil {
		return EventFeedResponse{Error: bindingErr}
	}
	subscribeCtx, cancelSubscribe := context.WithTimeout(ctx, a.callTimeout)
	subscription, err := manager.SubscribeEvents(subscribeCtx, request.DeviceID, request.TopicFilter)
	cancelSubscribe()
	if err != nil {
		finish()
		return EventFeedResponse{Error: bindingErrorFrom(err)}
	}
	id, err := newJobID()
	if err != nil {
		_ = subscription.Close(context.Background())
		finish()
		return EventFeedResponse{Error: bindingUnavailable("feed_failed", "the event feed could not be started")}
	}
	feedCtx, cancel := context.WithCancel(ctx)
	feed := &eventFeed{deviceID: request.DeviceID, cancel: cancel, state: jobRunning}
	diagnostics.mu.Lock()
	diagnostics.feeds[id] = feed
	diagnostics.mu.Unlock()
	go func() {
		defer finish()
		feed.run(feedCtx, manager, request, subscription)
	}()
	return EventFeedResponse{Result: &EventFeedState{FeedID: id, State: jobRunning, Events: []EventRecord{}}}
}

func (f *eventFeed) run(ctx context.Context, manager Manager, request StartEventsRequest, subscription core.EventSubscription) {
	failures := 0
	defer func() {
		if subscription != nil {
			closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = subscription.Close(closeCtx)
			cancel()
		}
		f.mu.Lock()
		if f.state == jobRunning {
			f.state = jobStopped
		}
		f.mu.Unlock()
	}()
	for ctx.Err() == nil {
		if subscription == nil {
			timer := time.NewTimer(eventRetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			subscribeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			next, err := manager.SubscribeEvents(subscribeCtx, request.DeviceID, request.TopicFilter)
			cancel()
			if err != nil {
				if f.recordFailure(ctx, err, &failures) {
					return
				}
				continue
			}
			subscription = next
		}
		pullCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		received, err := subscription.Pull(pullCtx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = subscription.Close(closeCtx)
			closeCancel()
			subscription = nil
			if f.recordFailure(ctx, err, &failures) {
				return
			}
			continue
		}
		failures = 0
		f.append(received)
	}
}

// recordFailure counts a failure and reports whether the feed must stop.
func (f *eventFeed) recordFailure(ctx context.Context, err error, failures *int) bool {
	if ctx.Err() != nil {
		return true
	}
	*failures++
	if *failures < maxEventRetries {
		return false
	}
	f.mu.Lock()
	f.state, f.failure = jobFailed, bindingErrorFrom(err)
	f.mu.Unlock()
	return true
}

func (f *eventFeed) append(received []core.EventDTO) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, event := range received {
		f.nextID++
		f.events = append(f.events, EventRecord{ID: f.nextID, ReceivedAt: now, EventDTO: event})
	}
	if overflow := len(f.events) - maxFeedEvents; overflow > 0 {
		f.events = append([]EventRecord(nil), f.events[overflow:]...)
	}
}

func (a *App) feed(id string) (*eventFeed, *BindingError) {
	if !validJobID(id) {
		return nil, bindingValidation("invalid_feed_id", "the event feed identifier is not valid")
	}
	a.diagnostics.mu.Lock()
	defer a.diagnostics.mu.Unlock()
	feed, ok := a.diagnostics.feeds[id]
	if !ok {
		return nil, bindingValidation("feed_not_found", "the event feed is not active")
	}
	return feed, nil
}

// PollEvents returns the feed state and the events after AfterID.
func (a *App) PollEvents(request EventFeedRequest) EventFeedResponse {
	feed, bindingErr := a.feed(request.FeedID)
	if bindingErr != nil {
		return EventFeedResponse{Error: bindingErr}
	}
	feed.mu.Lock()
	defer feed.mu.Unlock()
	state := &EventFeedState{FeedID: request.FeedID, State: feed.state, Error: feed.failure, Events: []EventRecord{}}
	for _, event := range feed.events {
		if event.ID > request.AfterID {
			state.Events = append(state.Events, event)
		}
	}
	return EventFeedResponse{Result: state}
}

// StopEvents unsubscribes and forgets a feed.
func (a *App) StopEvents(request EventFeedRequest) ActionResponse {
	feed, bindingErr := a.feed(request.FeedID)
	if bindingErr != nil {
		return ActionResponse{Error: bindingErr}
	}
	feed.cancel()
	a.diagnostics.mu.Lock()
	delete(a.diagnostics.feeds, request.FeedID)
	a.diagnostics.mu.Unlock()
	return ActionResponse{Result: &ActionResult{Completed: true}}
}

// --- Clock check ------------------------------------------------------------

type ClockCheckRequest struct {
	DeviceID     string `json:"device_id"`
	ProfileToken string `json:"profile_token"`
}

type ClockCheckResponse struct {
	Result *core.ClockCheckResult `json:"result,omitempty"`
	Error  *BindingError          `json:"error,omitempty"`
}

// ClockCheck measures the camera clock over ONVIF and RTCP.
func (a *App) ClockCheck(request ClockCheckRequest) ClockCheckResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return ClockCheckResponse{Error: err}
	}
	if request.ProfileToken != "" {
		if err := core.ValidateSnapshotProfileToken(request.ProfileToken); err != nil {
			return ClockCheckResponse{Error: bindingValidation("invalid_profile_token", "the profile token is not valid")}
		}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return ClockCheckResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.ClockCheck(ctx, request.DeviceID, request.ProfileToken, core.DefaultRTCPWait)
	if err != nil {
		return ClockCheckResponse{Error: bindingErrorFrom(err)}
	}
	a.diagnostics.mu.Lock()
	a.diagnostics.clock[request.DeviceID] = result
	a.diagnostics.mu.Unlock()
	return ClockCheckResponse{Result: &result}
}

// --- Stability test ---------------------------------------------------------

type StartStabilityRequest struct {
	DeviceID      string `json:"device_id"`
	ProfileToken  string `json:"profile_token"`
	Transport     string `json:"transport"`
	Cycles        int    `json:"cycles"`
	CycleSeconds  int    `json:"cycle_seconds"`
	PauseSeconds  int    `json:"pause_seconds"`
	AcceptLongRun bool   `json:"accept_long_run"`
}

type StabilityJobRequest struct {
	JobID string `json:"job_id"`
}

type StabilityJobState struct {
	JobID  string                `json:"job_id"`
	State  string                `json:"state"`
	Error  *BindingError         `json:"error,omitempty"`
	Cycles []core.StabilityCycle `json:"cycles"`
	Report *core.StabilityReport `json:"report,omitempty"`
}

type StabilityJobResponse struct {
	Result *StabilityJobState `json:"result,omitempty"`
	Error  *BindingError      `json:"error,omitempty"`
}

// StartStability runs a stability test in the background.
func (a *App) StartStability(request StartStabilityRequest) StabilityJobResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return StabilityJobResponse{Error: err}
	}
	if err := core.ValidateSnapshotProfileToken(request.ProfileToken); err != nil {
		return StabilityJobResponse{Error: bindingValidation("invalid_profile_token", "the profile token is not valid")}
	}
	transport := core.StreamTransport(request.Transport)
	if transport != core.StreamTransportTCP && transport != core.StreamTransportUDP {
		return StabilityJobResponse{Error: bindingValidation("invalid_transport", "the stream transport is not supported")}
	}
	options := core.StabilityOptions{
		Cycles: request.Cycles, CycleDuration: time.Duration(request.CycleSeconds) * time.Second,
		Pause: time.Duration(request.PauseSeconds) * time.Second,
	}
	if err := core.ValidateStabilityOptions(options); err != nil {
		return StabilityJobResponse{Error: bindingErrorFrom(err)}
	}
	a.diagnostics.mu.Lock()
	running := 0
	for _, job := range a.diagnostics.jobs {
		job.mu.Lock()
		if job.state == jobRunning {
			running++
		}
		job.mu.Unlock()
	}
	a.diagnostics.mu.Unlock()
	if running >= maxStabilityJobs {
		return StabilityJobResponse{Error: bindingValidation("too_many_jobs", "too many stability tests are running")}
	}
	id, err := newJobID()
	if err != nil {
		return StabilityJobResponse{Error: bindingUnavailable("job_failed", "the stability test could not be started")}
	}
	manager, ctx, finish, bindingErr := a.beginBackground()
	if bindingErr != nil {
		return StabilityJobResponse{Error: bindingErr}
	}
	jobCtx, cancel := context.WithCancel(ctx)
	job := &stabilityJob{deviceID: request.DeviceID, cancel: cancel, state: jobRunning, cycles: []core.StabilityCycle{}}
	a.diagnostics.mu.Lock()
	a.diagnostics.jobs[id] = job
	a.diagnostics.mu.Unlock()
	selection := core.StreamSelection{DeviceID: request.DeviceID, ProfileToken: request.ProfileToken, Transport: transport}
	go func() {
		defer finish()
		report, err := manager.StabilityTest(jobCtx, selection, options, func(cycle core.StabilityCycle) {
			job.mu.Lock()
			job.cycles = append(job.cycles, cycle)
			job.mu.Unlock()
		})
		job.mu.Lock()
		defer job.mu.Unlock()
		if err != nil {
			job.state, job.failure = jobFailed, bindingErrorFrom(err)
			return
		}
		job.report = &report
		job.state = jobCompleted
		if report.Verdict == core.StabilityIncomplete {
			job.state = jobStopped
		}
		if report.CyclesCompleted > 0 {
			a.diagnostics.mu.Lock()
			a.diagnostics.stability[request.DeviceID] = report
			a.diagnostics.mu.Unlock()
		}
	}()
	return StabilityJobResponse{Result: &StabilityJobState{JobID: id, State: jobRunning, Cycles: []core.StabilityCycle{}}}
}

func (a *App) job(id string) (*stabilityJob, *BindingError) {
	if !validJobID(id) {
		return nil, bindingValidation("invalid_job_id", "the stability test identifier is not valid")
	}
	a.diagnostics.mu.Lock()
	defer a.diagnostics.mu.Unlock()
	job, ok := a.diagnostics.jobs[id]
	if !ok {
		return nil, bindingValidation("job_not_found", "the stability test is not known")
	}
	return job, nil
}

// StabilityStatus reports a stability test's progress and final report.
func (a *App) StabilityStatus(request StabilityJobRequest) StabilityJobResponse {
	job, bindingErr := a.job(request.JobID)
	if bindingErr != nil {
		return StabilityJobResponse{Error: bindingErr}
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return StabilityJobResponse{Result: &StabilityJobState{
		JobID: request.JobID, State: job.state, Error: job.failure,
		Cycles: append([]core.StabilityCycle(nil), job.cycles...), Report: job.report,
	}}
}

// StopStability cancels a running stability test; its partial report stays
// available through StabilityStatus.
func (a *App) StopStability(request StabilityJobRequest) ActionResponse {
	job, bindingErr := a.job(request.JobID)
	if bindingErr != nil {
		return ActionResponse{Error: bindingErr}
	}
	job.cancel()
	return ActionResponse{Result: &ActionResult{Completed: true}}
}

// --- Reports ----------------------------------------------------------------

var errReportPath = errors.New("the report path is not valid")

type ExportReportRequest struct {
	DeviceID string `json:"device_id"`
}

// ReportExport is the saved report pair and the Markdown text for preview.
type ReportExport struct {
	MarkdownPath string            `json:"markdown_path"`
	JSONPath     string            `json:"json_path"`
	Markdown     string            `json:"markdown"`
	Report       core.DeviceReport `json:"report"`
}

type ReportExportResponse struct {
	Result *ReportExport `json:"result,omitempty"`
	Error  *BindingError `json:"error,omitempty"`
}

type RevealReportRequest struct {
	Path string `json:"path"`
}

func reportDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Documents", "ovm"), nil
}

// ExportReport builds the device report, including this session's last clock
// check and stability test of the device, and saves it as Markdown and JSON.
func (a *App) ExportReport(request ExportReportRequest) ReportExportResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return ReportExportResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return ReportExportResponse{Error: bindingErr}
	}
	defer finish()
	options := core.ReportOptions{Generator: "ovm " + buildinfo.Current().Version}
	a.diagnostics.mu.Lock()
	if clock, ok := a.diagnostics.clock[request.DeviceID]; ok {
		options.Clock = &clock
	}
	if stability, ok := a.diagnostics.stability[request.DeviceID]; ok {
		options.Stability = &stability
	}
	a.diagnostics.mu.Unlock()
	report, err := manager.BuildReport(ctx, request.DeviceID, options)
	if err != nil {
		return ReportExportResponse{Error: bindingErrorFrom(err)}
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return ReportExportResponse{Error: bindingUnavailable("report_failed", "the report could not be encoded")}
	}
	markdown := core.RenderReportMarkdown(report)
	directory, err := a.reportDir()
	if err != nil {
		return ReportExportResponse{Error: bindingUnavailable("report_failed", "the reports folder is not available")}
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ReportExportResponse{Error: bindingUnavailable("report_failed", "the reports folder could not be created")}
	}
	base := reportFileBase(report, time.Now())
	export := &ReportExport{
		MarkdownPath: filepath.Join(directory, base+".md"), JSONPath: filepath.Join(directory, base+".json"),
		Markdown: markdown, Report: report,
	}
	if err := writeNewFile(export.MarkdownPath, []byte(markdown)); err != nil {
		return ReportExportResponse{Error: bindingUnavailable("report_failed", "the report could not be saved")}
	}
	if err := writeNewFile(export.JSONPath, append(encoded, '\n')); err != nil {
		return ReportExportResponse{Error: bindingUnavailable("report_failed", "the report could not be saved")}
	}
	return ReportExportResponse{Result: export}
}

// reportFileBase names a report from the camera model and local time; only
// [A-Za-z0-9_-] survive from the model.
func reportFileBase(report core.DeviceReport, now time.Time) string {
	var builder strings.Builder
	for _, r := range report.Device.Model {
		if builder.Len() >= 32 {
			break
		}
		if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			builder.WriteRune(r)
		}
	}
	model := builder.String()
	if model == "" {
		model = "camera"
	}
	return "ovm-report-" + model + "-" + now.Format("20060102-150405")
}

// writeNewFile writes a private file that must not already exist.
func writeNewFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: path is built from the reports folder and a sanitized name
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func validateReportPath(directory, path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return "", errReportPath
	}
	clean := filepath.Clean(path)
	name := filepath.Base(clean)
	if filepath.Dir(clean) != filepath.Clean(directory) || !strings.HasPrefix(name, "ovm-report-") ||
		(!strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".json")) {
		return "", errReportPath
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.Mode().IsRegular() {
		return "", errReportPath
	}
	return clean, nil
}

// RevealReport shows a saved report in the platform file manager.
func (a *App) RevealReport(request RevealReportRequest) ActionResponse {
	a.mu.Lock()
	files, closed := a.files, a.closed
	a.mu.Unlock()
	if closed || files == nil {
		return ActionResponse{Error: bindingUnavailable("app_closed", "the desktop application is closed")}
	}
	directory, err := a.reportDir()
	if err != nil {
		return ActionResponse{Error: bindingValidation("invalid_report_path", errReportPath.Error())}
	}
	path, err := validateReportPath(directory, request.Path)
	if err != nil {
		return ActionResponse{Error: bindingValidation("invalid_report_path", errReportPath.Error())}
	}
	executable, arguments, err := revealCommand(runtime.GOOS, path)
	if err != nil {
		return ActionResponse{Error: bindingErrorFrom(err)}
	}
	if err := files.Start(context.Background(), executable, arguments); err != nil {
		return ActionResponse{Error: bindingUnavailable("reveal_failed", "the file manager could not be opened")}
	}
	return ActionResponse{Result: &ActionResult{Completed: true}}
}
