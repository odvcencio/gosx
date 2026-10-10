package browser

import (
	"context"
	"errors"
)

var ErrFetchUnavailable = errors.New("browser: fetch unavailable")

// FetchRequest describes one byte-oriented browser HTTP request. A nil Body
// omits the request body; a non-nil empty Body sends an empty byte array.
type FetchRequest struct {
	URL         string
	Method      string
	ContentType string
	Headers     map[string]string
	Body        []byte
}

// FetchResponse preserves HTTP errors as responses. Fetch returns an error
// for transport, cancellation or body-reading failures, not non-2xx statuses.
type FetchResponse struct {
	Status     int
	StatusText string
	Body       []byte
}

func fetchContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("browser: fetch context required")
	}
	return ctx.Err()
}
