package httpadapter

import "fmt"

// StatusError wraps a non-2xx HTTP response from a device endpoint,
// carrying the status code so callers (device_service.go's
// httpStatusError) can map it to an appropriate gRPC status — req42.adoc
// F-10: "HTTP errors (4xx/5xx) mapped to appropriate gRPC status codes".
type StatusError struct {
	StatusCode int
	Status     string // e.g. "404 Not Found"
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("http: device returned %s", e.Status)
}
