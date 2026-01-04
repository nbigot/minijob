package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"go.uber.org/zap"
)

// CallbackPayload represents the data sent to the callback URL
type CallbackPayload struct {
	JobID     string   `json:"jobId"`
	Status    string   `json:"status"`    // "success", "failed", "canceled"
	Timestamp int64    `json:"timestamp"` // Unix timestamp in milliseconds
	Job       *job.Job `json:"job"`       // Full job details
}

// CallbackService handles job completion callbacks
type CallbackService struct {
	// Implements IServiceEventObserver interface
	httpClient *http.Client
	maxRetries int
	retryDelay time.Duration
	logger     *zap.Logger
}

// Init implements IServiceEventObserver interface
func (cs *CallbackService) Init() error {
	return nil
}

// Shutdown implements IServiceEventObserver interface
func (cs *CallbackService) Shutdown() {
	// No cleanup needed for callback service
}

// NotifyEvent implements IServiceEventObserver interface
func (cs *CallbackService) NotifyEvent(ev event.ServiceEventType) {
	// No action needed for general events
}

// NotifyTopicEvent implements IServiceEventObserver interface
func (cs *CallbackService) NotifyTopicEvent(ev event.ServiceEventType, topic string) {
	// No action needed for topic events
}

// NotifyJobEvent implements IServiceEventObserver interface
func (cs *CallbackService) NotifyJobEvent(j *job.Job, ev event.ServiceEventType) {
	// Check if this is a job completion event and trigger callback if needed
	if cs.ShouldTriggerCallback(j) {
		cs.ExecuteCallbackAsync(j)
	}
}

// ShouldTriggerCallback checks if a callback should be triggered based on job state
func (cs *CallbackService) ShouldTriggerCallback(j *job.Job) bool {
	if j.CallbackURL == "" {
		return false
	}

	// Trigger callback only for final states
	return j.IsCompleted()
}

// ExecuteCallback sends the callback notification
func (cs *CallbackService) ExecuteCallback(job *job.Job) error {
	// Additional safety check
	if job.CallbackURL == "" {
		return fmt.Errorf("callback URL is empty for job %s", job.JobUUID.String())
	}

	payload := CallbackPayload{
		JobID:     job.JobUUID.String(),
		Status:    job.GetStateString(),
		Timestamp: job.GetLastUpdateDate(),
		Job:       job,
	}

	return cs.sendCallback(job.CallbackURL, payload)
}

// ExecuteCallbackAsync sends the callback notification asynchronously
func (cs *CallbackService) ExecuteCallbackAsync(job *job.Job) {
	go func() {
		if err := cs.ExecuteCallback(job); err != nil {
			if cs.logger != nil {
				cs.logger.Error("Callback failed",
					zap.String("jobId", job.JobUUID.String()),
					zap.Error(err))
			}
		} else {
			if cs.logger != nil {
				cs.logger.Info("Callback successfully sent",
					zap.String("jobId", job.JobUUID.String()))
			}
		}
	}()
}

// sendCallback sends the HTTP POST request to the callback URL with retries
func (cs *CallbackService) sendCallback(callbackURL string, payload CallbackPayload) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal callback payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= cs.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(cs.retryDelay)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, err := http.NewRequestWithContext(ctx, "POST", callbackURL, bytes.NewBuffer(jsonData))
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "MiniJob-Callback/1.0")

		resp, err := cs.httpClient.Do(req)
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			continue
		}

		if err := resp.Body.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close response body: %w", err)
			continue
		}

		// Consider 2xx status codes as success
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		lastErr = fmt.Errorf("callback returned status %d", resp.StatusCode)
	}

	return fmt.Errorf("failed to send callback after %d attempts: %w", cs.maxRetries+1, lastErr)
}

// SetLogger sets the logger for the callback service
func (cs *CallbackService) SetLogger(logger *zap.Logger) {
	cs.logger = logger
}

// NewCallbackService creates a new callback service
func NewCallbackService(logger *zap.Logger) *CallbackService {
	return &CallbackService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		maxRetries: 3,
		retryDelay: 2 * time.Second,
		logger:     logger,
	}
}
