package middleware

import (
	"os"
	"time"

	"code.google.com/p/go-uuid/uuid"
	"github.com/achilleasa/usrv"
	"golang.org/x/net/context"
)

type TraceType string

// The types of traces that are emitted by the Trace middleware.
const (
	Request  TraceType = "REQ"
	Response TraceType = "RES"
)

// The TraceLog structure represents a trace entry
// that is emitted by the Trace middleware.
type TraceLog struct {
	Timestamp     time.Time `json:"timestamp"`
	TraceId       string    `json:"trace_id"`
	CorrelationId string    `json:"correlation_id"`
	Type          TraceType `json:"type"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	Host          string    `json:"host"`
	Duration      int64     `json:"duration,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// The trace middleware emits TraceLog events to traceChan whenever the
// server processes an incoming request.
//
// Two TraceLog entries will be emitted for each request, one for the incoming request
// and one for the outgoing response.
//
// The middleware will inject a traceId for each incoming request into the context
// that gets passed to the request handler. To ensure that any further RPC requests
// that occur inside the wrapped handler are associated with the current request, the
// handler should pass its context to any performed RPC client requests.
//
// This function is designed to emit events in non-blocking mode. If traceChan does
// not have enough capacity to store a generated TraceLog message then it will be
// silently dropped. Consequently, it is a good practice to ensure that traceChan is a
// buffered channel.
func Trace(traceChan chan TraceLog) usrv.EndpointOption {
	return func(ep *usrv.Endpoint) error {
		hostname, err := os.Hostname()
		if err != nil {
			return err
		}

		// Wrap original method
		originalHandler := ep.Handler
		ep.Handler = usrv.HandlerFunc(func(ctx context.Context, responseWriter usrv.ResponseWriter, request *usrv.Message) {
			var traceId string

			// Check if the request contains a trace id. If no trace is
			// available allocate a new traceId and inject it in the
			// request context that gets passed to the handler
			trace := request.Headers.Get(usrv.CtxTraceId)
			if trace == nil {
				traceId = uuid.New()
				ctx = context.WithValue(ctx, usrv.CtxTraceId, traceId)
			} else {
				traceId = trace.(string)
			}

			// Inject trace into outgoing message
			responseWriter.Header().Set(usrv.CtxTraceId, traceId)

			// Trace incoming request. Use a select statement to ensure write is non-blocking.
			traceEntry := TraceLog{
				Timestamp:     time.Now(),
				TraceId:       traceId,
				CorrelationId: request.CorrelationId,
				Type:          Request,
				From:          request.From,
				To:            request.To,
				Host:          hostname,
			}
			select {
			case traceChan <- traceEntry:
				// trace successfully added to channel
			default:
				// channel is full, skip trace
			}

			// Trace response when the handler returns
			defer func(start time.Time) {

				var errMsg string

				errVal := responseWriter.Header().Get("error")
				if errVal != nil {
					errMsg = errVal.(string)
				}

				traceEntry := TraceLog{
					Timestamp:     time.Now(),
					TraceId:       traceId,
					CorrelationId: request.CorrelationId,
					Type:          Response,
					From:          request.To, // when responding we switch From/To
					To:            request.From,
					Host:          hostname,
					Duration:      time.Since(start).Nanoseconds(),
					Error:         errMsg,
				}

				select {
				case traceChan <- traceEntry:
				// trace successfully added to channel
				default:
					// channel is full, skip trace
				}
			}(time.Now())

			// Invoke the original handler
			originalHandler.Serve(ctx, responseWriter, request)
		})

		return nil
	}
}
