package core

import "strings"

type FailureReason string

const (
	FailureAuth          FailureReason = "auth"
	FailureAuthPermanent FailureReason = "auth_permanent"
	FailureFormat        FailureReason = "format"
	FailureRateLimit     FailureReason = "rate_limit"
	FailureBilling       FailureReason = "billing"
	FailureTimeout       FailureReason = "timeout"
	FailureModelNotFound FailureReason = "model_not_found"
	FailureModelCapacity FailureReason = "model_capacity"
	FailureUnknown       FailureReason = "unknown"
)

func ClassifyFailure(message string) FailureReason {
	lower := strings.ToLower(message)

	switch {
	case strings.Contains(lower, "401"),
		strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "invalid token"),
		strings.Contains(lower, "permission denied"):
		return FailureAuth
	case strings.Contains(lower, "suspended"),
		strings.Contains(lower, "forbidden permanently"):
		return FailureAuthPermanent
	case strings.Contains(lower, "402"),
		strings.Contains(lower, "billing"),
		strings.Contains(lower, "quota exceeded"):
		return FailureBilling
	case strings.Contains(lower, "overloaded"),
		strings.Contains(lower, "no capacity"),
		strings.Contains(lower, "model_capacity"),
		strings.Contains(lower, "model capacity"):
		return FailureModelCapacity
	case strings.Contains(lower, "429"),
		strings.Contains(lower, "rate limit"),
		strings.Contains(lower, "resource exhausted"):
		return FailureRateLimit
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "deadline exceeded"):
		return FailureTimeout
	case strings.Contains(lower, "404"),
		strings.Contains(lower, "model not found"):
		return FailureModelNotFound
	case strings.Contains(lower, "invalid argument"),
		strings.Contains(lower, "bad request"),
		strings.Contains(lower, "schema"):
		return FailureFormat
	default:
		return FailureUnknown
	}
}

func IsRetriable(reason FailureReason) bool {
	switch reason {
	case FailureRateLimit, FailureTimeout, FailureUnknown, FailureAuth, FailureBilling:
		return true
	default:
		return false
	}
}
