package middleware

import (
	"errors"
	"time"

	"github.com/achilleasa/usrv"
	"golang.org/x/net/context"
)

// Apply a throttling middleware to the input handler that throttles
// request handling to maxConcurrent requests.
//
// If a non-zero timeout is specified and a pending request cannot be
// serviced within the specified timeout, it will be aborted with
// ErrTimeout.
func Throttle(maxConcurrent int, timeout time.Duration) usrv.EndpointOption {

	return func(ep *usrv.Endpoint) error {

		if maxConcurrent <= 0 {
			return errors.New("maxConcurrent should be > 0")
		}

		// Allocate a buffered channel and pre-fill it with tokens
		tokens := make(chan struct{}, maxConcurrent)
		for i := 0; i < maxConcurrent; i++ {
			tokens <- struct{}{}
		}

		// Wrap original method
		originalHandler := ep.Handler
		ep.Handler = usrv.HandlerFunc(func(ctx context.Context, responseWriter usrv.ResponseWriter, request *usrv.Message) {
			if timeout > 0 {
				var cancelFunc context.CancelFunc
				ctx, cancelFunc = context.WithTimeout(ctx, timeout)
				defer cancelFunc()
			}

			select {
			case <-tokens:
				// We got a token, execute request
				originalHandler.Serve(ctx, responseWriter, request)

				// Return back token
				tokens <- struct{}{}
			case <-ctx.Done():
				responseWriter.WriteError(ctx.Err())
			}

		})

		return nil
	}
}
