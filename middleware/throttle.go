package middleware

import (
	"errors"
	"github.com/achilleasa/usrv"
	"golang.org/x/net/context"
	"time"
)

// Apply a throttling middleware to the input handler and return a
// new handler which limits request processing to maxConcurrent requests
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
			fctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			select {
			case <-tokens:
				// We got a token, execute request
				originalHandler.Serve(fctx, responseWriter, request)

				// Return back token
				tokens <- struct{}{}
			case <-fctx.Done():
				responseWriter.WriteError(usrv.ErrTimeout)
			}

		})

		return nil
	}
}
