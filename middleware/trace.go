package middleware

import (
	"code.google.com/p/go-uuid/uuid"
	"encoding/json"
	"github.com/achilleasa/usrv"
	"golang.org/x/net/context"
	"log"
	"os"
	"time"
)

type traceType string

const (
	Request  traceType = "REQ"
	Response traceType = "RES"
)

type traceLog struct {
	Timestamp     time.Time `json:"timestamp"`
	TraceId       string    `json:"trace_id"`
	CorrelationId string    `json:"correlation_id"`
	Type          traceType `json:"type"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	Host          string    `json:"host"`
	Duration      int64     `json:"duration"`
}

func Trace(logger *log.Logger) usrv.EndpointOption {
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
			trace := request.Headers[usrv.CtxTraceId]
			if trace == nil {
				traceId = uuid.New()
				ctx = context.WithValue(ctx, usrv.CtxTraceId, traceId)
			} else {
				traceId = trace.(string)
			}

			// Inject trace into outgoing message
			responseWriter.WriteHeader(usrv.CtxTraceId, traceId)

			// Trace incoming request
			logEntry := traceLog{
				Timestamp:     time.Now(),
				TraceId:       traceId,
				CorrelationId: request.CorrelationId,
				Type:          Request,
				From:          request.From,
				To:            request.To,
				Host:          hostname,
			}

			entry, _ := json.Marshal(logEntry)
			logger.Printf("%s", entry)

			// Trace response when the handler returns
			defer func(start time.Time) {
				logEntry := traceLog{
					Timestamp:     time.Now(),
					TraceId:       traceId,
					CorrelationId: request.CorrelationId,
					Type:          Response,
					From:          request.To, // when responding we switch From/To
					To:            request.From,
					Host:          hostname,
					Duration:      time.Since(start).Nanoseconds(),
				}

				entry, _ := json.Marshal(logEntry)
				logger.Printf("%s", entry)

			}(time.Now())

			// Invoke the original handler
			originalHandler.Serve(ctx, responseWriter, request)
		})

		return nil
	}
}
