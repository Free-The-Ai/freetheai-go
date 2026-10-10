package freetheai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Public links the client mentions in its error messages.
const (
	CheckinURL = "https://freetheai.org/checkin"
	KeysURL    = "https://freetheai.org/dashboard/api-keys"
	PlanURL    = "https://freetheai.org/dashboard/plan"
)

// APIError is the base error for every answer the API gives with an error status.
// The specific types below embed it, so errors.As works with either.
type APIError struct {
	StatusCode int
	Code       string
	// Message is the message as the API wrote it.
	Message string
	// ErrorID identifies the failure. Quote it when you ask for help.
	ErrorID string
	// RetryAfter is the wait the API asked for, when it sent Retry-After.
	RetryAfter time.Duration
	// Body is the raw response body.
	Body []byte
}

func (e *APIError) Error() string {
	text := fmt.Sprintf("%d: %s", e.StatusCode, e.Message)
	if e.ErrorID != "" && !strings.Contains(e.Message, e.ErrorID) {
		text += fmt.Sprintf(" (error id %s)", e.ErrorID)
	}
	return text
}

// BadRequestError is a 400: the request was invalid.
type BadRequestError struct{ APIError }

// Unwrap returns the base APIError.
func (e *BadRequestError) Unwrap() error { return &e.APIError }

// ContentFilterError is a 400: the model's content filter refused the request or its reply.
// Rephrasing or another model usually works.
type ContentFilterError struct{ APIError }

// Unwrap returns the base APIError.
func (e *ContentFilterError) Unwrap() error { return &e.APIError }

// ImagesNotSupportedError is a 400: the model reads text only.
type ImagesNotSupportedError struct{ APIError }

// Unwrap returns the base APIError.
func (e *ImagesNotSupportedError) Unwrap() error { return &e.APIError }

// RequestTooLargeError is a 413: the request body is over the size limit.
type RequestTooLargeError struct{ APIError }

// Unwrap returns the base APIError.
func (e *RequestTooLargeError) Unwrap() error { return &e.APIError }

// AuthenticationError is a 401: the API key is missing, wrong, or revoked.
type AuthenticationError struct{ APIError }

// Unwrap returns the base APIError.
func (e *AuthenticationError) Unwrap() error { return &e.APIError }

// IncompleteKeyError means the key is only part of a key. Keys are shown once.
type IncompleteKeyError struct{ APIError }

// Unwrap returns the base APIError.
func (e *IncompleteKeyError) Unwrap() error { return &e.APIError }

// PermissionDeniedError is a 403: this key may not do this.
type PermissionDeniedError struct{ APIError }

// Unwrap returns the base APIError.
func (e *PermissionDeniedError) Unwrap() error { return &e.APIError }

// CheckinRequiredError is a 403: free models need the daily check-in on the website first.
type CheckinRequiredError struct{ APIError }

// CheckinURL returns the page where the check-in is done.
func (e *CheckinRequiredError) CheckinURL() string { return CheckinURL }

// Unwrap returns the base APIError.
func (e *CheckinRequiredError) Unwrap() error { return &e.APIError }

// EmailNotVerifiedError is a 403: confirm the account's email address before using the API.
type EmailNotVerifiedError struct{ APIError }

// Unwrap returns the base APIError.
func (e *EmailNotVerifiedError) Unwrap() error { return &e.APIError }

// NotFoundError is a 404: unknown task or route.
type NotFoundError struct{ APIError }

// Unwrap returns the base APIError.
func (e *NotFoundError) Unwrap() error { return &e.APIError }

// ModelNotFoundError is a 404: no model has this ID. Suggestions lists close model IDs.
type ModelNotFoundError struct {
	APIError
	Suggestions []string
}

// Unwrap returns the base APIError.
func (e *ModelNotFoundError) Unwrap() error { return &e.APIError }

// RateLimitError is a 429: too many requests.
type RateLimitError struct{ APIError }

// Unwrap returns the base APIError.
func (e *RateLimitError) Unwrap() error { return &e.APIError }

// ConcurrencyLimitError is a 429: the account already has as many replies running as its plan allows.
type ConcurrencyLimitError struct{ APIError }

// Unwrap returns the base APIError.
func (e *ConcurrencyLimitError) Unwrap() error { return &e.APIError }

// DailyLimitError is a 429: today's free requests are used up.
type DailyLimitError struct{ APIError }

// ResetsAt returns the next 00:00 UTC, when the daily allowance resets.
func (e *DailyLimitError) ResetsAt() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}

// Unwrap returns the base APIError.
func (e *DailyLimitError) Unwrap() error { return &e.APIError }

// ProviderBudgetError is a 429: today's token budget for this model is used up.
// Models from other providers still work.
type ProviderBudgetError struct{ APIError }

// Unwrap returns the base APIError.
func (e *ProviderBudgetError) Unwrap() error { return &e.APIError }

// InternalServerError is a 5xx: the service is temporarily unavailable.
type InternalServerError struct{ APIError }

// Unwrap returns the base APIError.
func (e *InternalServerError) Unwrap() error { return &e.APIError }

// ModelBusyError is a 503: every route for the model is busy.
type ModelBusyError struct{ APIError }

// Unwrap returns the base APIError.
func (e *ModelBusyError) Unwrap() error { return &e.APIError }

// ConnectionError means no answer came back (network error or timeout).
type ConnectionError struct{ Message string }

func (e *ConnectionError) Error() string { return e.Message }

// StreamInterruptedError means a streamed reply stopped before the model finished.
// Partial holds the text that arrived before the cut.
type StreamInterruptedError struct {
	Message string
	ErrorID string
	Partial string
}

func (e *StreamInterruptedError) Error() string {
	if e.ErrorID != "" {
		return fmt.Sprintf("%s (error id %s)", e.Message, e.ErrorID)
	}
	return e.Message
}

// TaskFailedError means an image or video task ended without output.
type TaskFailedError struct {
	Message string
	Code    string
	ErrorID string
	Task    *Task
}

func (e *TaskFailedError) Error() string {
	if e.ErrorID != "" {
		return fmt.Sprintf("%s (error id %s)", e.Message, e.ErrorID)
	}
	return e.Message
}

// errorFor builds the matching error from a status, a body, and the response headers.
func errorFor(status int, body []byte, header http.Header) error {
	var message, code, errorID string
	var parsed map[string]any
	isJSON := json.Unmarshal(body, &parsed) == nil && parsed != nil
	if isJSON {
		detail, _ := parsed["error"].(map[string]any)
		if detail == nil {
			detail = parsed
		}
		message, _ = detail["message"].(string)
		code, _ = detail["code"].(string)
		if code == "" {
			code, _ = detail["type"].(string)
		}
		errorID, _ = detail["error_id"].(string)
	}
	if message == "" {
		// Not the API's JSON: usually an edge page (Cloudflare 52x) during a restart.
		message = "request failed"
		if !isJSON {
			text := strings.TrimSpace(string(body))
			if text != "" {
				message = truncate(text, 200)
			}
		}
		if status >= 500 || strings.Contains(strings.ToLower(string(body)), "<html") {
			message = "the service is temporarily unreachable"
		}
	}
	if errorID == "" {
		errorID = header.Get("X-Error-ID")
	}
	base := APIError{
		StatusCode: status,
		Code:       code,
		Message:    message,
		ErrorID:    errorID,
		RetryAfter: parseRetryAfter(header.Get("Retry-After")),
		Body:       body,
	}
	return classify(base)
}

func classify(base APIError) error {
	text := strings.ToLower(base.Message)
	switch {
	case base.StatusCode == 400:
		switch {
		case base.Code == "content_filter" || strings.HasPrefix(text, "content filtered"):
			return &ContentFilterError{base}
		case strings.Contains(text, "accepts text only"):
			return &ImagesNotSupportedError{base}
		}
		return &BadRequestError{base}
	case base.StatusCode == 401:
		if strings.Contains(text, "not a complete freetheai api key") {
			return &IncompleteKeyError{base}
		}
		return &AuthenticationError{base}
	case base.StatusCode == 403:
		switch {
		case strings.Contains(text, "daily check-in"):
			return &CheckinRequiredError{base}
		case strings.Contains(text, "confirm your email"):
			return &EmailNotVerifiedError{base}
		}
		return &PermissionDeniedError{base}
	case base.StatusCode == 404:
		if base.Code == "model_not_found" || strings.Contains(text, "was not found") || strings.Contains(text, "model not found") {
			return &ModelNotFoundError{APIError: base}
		}
		return &NotFoundError{base}
	case base.StatusCode == 413:
		return &RequestTooLargeError{base}
	case base.StatusCode == 429:
		switch {
		case strings.Contains(text, "daily free request limit"):
			return &DailyLimitError{base}
		case strings.Contains(text, "token budget"):
			return &ProviderBudgetError{base}
		case strings.Contains(text, "concurrency limit"):
			return &ConcurrencyLimitError{base}
		}
		return &RateLimitError{base}
	case base.StatusCode >= 500:
		if strings.Contains(text, "busy") || strings.Contains(text, "at capacity") {
			return &ModelBusyError{base}
		}
		return &InternalServerError{base}
	}
	return &base
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func truncate(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[:n]
}
