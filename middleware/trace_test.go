package middleware

import (
	"testing"

	"time"

	"errors"

	"reflect"
	"sort"

	"github.com/achilleasa/usrv"
	"github.com/achilleasa/usrv/usrvtest"
	"golang.org/x/net/context"
)

func TestTracerWithoutTraceId(t *testing.T) {
	ep := usrv.Endpoint{
		Name: "traceTest",
		Handler: usrv.HandlerFunc(func(ctx context.Context, rw usrv.ResponseWriter, req *usrv.Message) {
		}),
	}

	var err error

	traceChan := make(chan TraceEntry, 10)
	err = Tracer(traceChan)(&ep)
	if err != nil {
		t.Fatalf("Error applying Tracer() to endpoint: %v", err)
	}

	msg := &usrv.Message{
		From:          "sender",
		To:            "recipient",
		CorrelationId: "123",
	}

	// Send a request without a trace id
	w := usrvtest.NewRecorder()
	ep.Handler.Serve(context.Background(), w, msg)

	traceId := w.Header().Get(usrv.CtxTraceId)
	if traceId == nil {
		t.Fatalf("Expected middleware to set response writer header %s", usrv.CtxTraceId)
	}

	// Fetch generated tracelogs
	var traceIn, traceOut TraceEntry
	select {
	case traceIn = <-traceChan:
	case <-time.After(time.Second * 1):
		t.Fatalf("Could not retrieve trace REQ entry after 1 second")
	}
	select {
	case traceOut = <-traceChan:
	case <-time.After(time.Second * 1):
		t.Fatalf("Could not retrieve trace RES entry after 1 second")
	}

	// Validate REQ trace
	if traceIn.Type != Request {
		t.Fatalf("Expected trace to be of type %v; got %v", Request, traceIn.Type)
	}
	if traceIn.CorrelationId != msg.CorrelationId {
		t.Fatalf("Expected trace CorrelationId to be %s; got %s", msg.CorrelationId, traceIn.CorrelationId)
	}
	if traceIn.TraceId != traceId {
		t.Fatalf("Expected trace TraceId to be %s; got %s", traceId, traceIn.TraceId)
	}
	if traceIn.Error != "" {
		t.Fatalf("Expected trace Error to be ''; got %v", traceIn.Error)
	}
	if traceIn.From != msg.From {
		t.Fatalf("Expected trace From to be %s; got %s", msg.From, traceIn.From)
	}
	if traceIn.To != msg.To {
		t.Fatalf("Expected trace To to be %s; got %s", msg.To, traceIn.To)
	}

	// Validate RES trace
	if traceOut.Type != Response {
		t.Fatalf("Expected trace to be of type %v; got %v", Response, traceOut.Type)
	}
	if traceOut.CorrelationId != msg.CorrelationId {
		t.Fatalf("Expected trace CorrelationId to be %s; got %s", msg.CorrelationId, traceOut.CorrelationId)
	}
	if traceOut.TraceId != traceId {
		t.Fatalf("Expected trace TraceId to be %s; got %s", traceId, traceOut.TraceId)
	}
	if traceOut.Error != "" {
		t.Fatalf("Expected trace Error to be ''; got %v", traceOut.Error)
	}
	// Out trace should reverse From and To
	if traceOut.From != msg.To {
		t.Fatalf("Expected trace From to be %s; got %s", msg.To, traceOut.From)
	}
	if traceOut.To != msg.From {
		t.Fatalf("Expected trace To to be %s; got %s", msg.From, traceOut.To)
	}

}

func TestTracerWithExistingTraceId(t *testing.T) {
	ep := usrv.Endpoint{
		Name: "traceTest",
		Handler: usrv.HandlerFunc(func(ctx context.Context, rw usrv.ResponseWriter, req *usrv.Message) {
		}),
	}

	var err error

	traceChan := make(chan TraceEntry, 10)
	err = Tracer(traceChan)(&ep)
	if err != nil {
		t.Fatalf("Error applying Tracer() to endpoint: %v", err)
	}

	msg := &usrv.Message{
		From:          "sender",
		To:            "recipient",
		CorrelationId: "123",
		Headers:       make(usrv.Header),
	}

	// Send a request with an existing trace id
	existingTraceId := "0-0-0-0"
	msg.Headers.Set(usrv.CtxTraceId, existingTraceId)

	w := usrvtest.NewRecorder()
	ep.Handler.Serve(context.Background(), w, msg)

	traceId := w.Header().Get(usrv.CtxTraceId)
	if traceId == nil {
		t.Fatalf("Expected middleware to set response writer header %s", usrv.CtxTraceId)
	}
	if traceId != existingTraceId {
		t.Fatalf("Middleware did not reuse existing traceId %s; got %s", existingTraceId, traceId)
	}

	// Fetch generated tracelogs
	var traceIn, traceOut TraceEntry
	select {
	case traceIn = <-traceChan:
	case <-time.After(time.Second * 1):
		t.Fatalf("Could not retrieve trace REQ entry after 1 second")
	}
	select {
	case traceOut = <-traceChan:
	case <-time.After(time.Second * 1):
		t.Fatalf("Could not retrieve trace RES entry after 1 second")
	}

	// Validate REQ trace
	if traceIn.Type != Request {
		t.Fatalf("Expected trace to be of type %v; got %v", Request, traceIn.Type)
	}
	if traceIn.CorrelationId != msg.CorrelationId {
		t.Fatalf("Expected trace CorrelationId to be %s; got %s", msg.CorrelationId, traceIn.CorrelationId)
	}
	if traceIn.TraceId != traceId {
		t.Fatalf("Expected trace TraceId to be %s; got %s", traceId, traceIn.TraceId)
	}
	if traceIn.Error != "" {
		t.Fatalf("Expected trace Error to be ''; got %v", traceIn.Error)
	}
	if traceIn.From != msg.From {
		t.Fatalf("Expected trace From to be %s; got %s", msg.From, traceIn.From)
	}
	if traceIn.To != msg.To {
		t.Fatalf("Expected trace To to be %s; got %s", msg.To, traceIn.To)
	}

	// Validate RES trace
	if traceOut.Type != Response {
		t.Fatalf("Expected trace to be of type %v; got %v", Response, traceOut.Type)
	}
	if traceOut.CorrelationId != msg.CorrelationId {
		t.Fatalf("Expected trace CorrelationId to be %s; got %s", msg.CorrelationId, traceOut.CorrelationId)
	}
	if traceOut.TraceId != traceId {
		t.Fatalf("Expected trace TraceId to be %s; got %s", traceId, traceOut.TraceId)
	}
	if traceOut.Error != "" {
		t.Fatalf("Expected trace Error to be ''; got %v", traceOut.Error)
	}
	// Out trace should reverse From and To
	if traceOut.From != msg.To {
		t.Fatalf("Expected trace From to be %s; got %s", msg.To, traceOut.From)
	}
	if traceOut.To != msg.From {
		t.Fatalf("Expected trace To to be %s; got %s", msg.From, traceOut.To)
	}

}

func TestTracerWithError(t *testing.T) {
	ep := usrv.Endpoint{
		Name: "traceTest",
		Handler: usrv.HandlerFunc(func(ctx context.Context, rw usrv.ResponseWriter, req *usrv.Message) {
			rw.WriteError(errors.New("I cannot allow you to do that Dave"))
		}),
	}

	var err error

	traceChan := make(chan TraceEntry, 10)
	err = Tracer(traceChan)(&ep)
	if err != nil {
		t.Fatalf("Error applying Tracer() to endpoint: %v", err)
	}

	msg := &usrv.Message{
		From:          "sender",
		To:            "recipient",
		CorrelationId: "123",
	}

	// Send request
	w := usrvtest.NewRecorder()
	ep.Handler.Serve(context.Background(), w, msg)

	traceId := w.Header().Get(usrv.CtxTraceId)
	if traceId == nil {
		t.Fatalf("Expected middleware to set response writer header %s", usrv.CtxTraceId)
	}

	// Fetch generated tracelogs
	var traceOut TraceEntry
	select {
	case _ = <-traceChan:
	case <-time.After(time.Second * 1):
		t.Fatalf("Could not retrieve trace REQ entry after 1 second")
	}
	select {
	case traceOut = <-traceChan:
	case <-time.After(time.Second * 1):
		t.Fatalf("Could not retrieve trace RES entry after 1 second")
	}

	// Validate RES trace
	if traceOut.Type != Response {
		t.Fatalf("Expected trace to be of type %v; got %v", Response, traceOut.Type)
	}
	if traceOut.CorrelationId != msg.CorrelationId {
		t.Fatalf("Expected trace CorrelationId to be %s; got %s", msg.CorrelationId, traceOut.CorrelationId)
	}
	if traceOut.TraceId != traceId {
		t.Fatalf("Expected trace TraceId to be %s; got %s", traceId, traceOut.TraceId)
	}
	if traceOut.Error != "I cannot allow you to do that Dave" {
		t.Fatalf("Expected trace Error to be 'I cannot allow you to do that Dave'; got %v", traceOut.Error)
	}
	// Out trace should reverse From and To
	if traceOut.From != msg.To {
		t.Fatalf("Expected trace From to be %s; got %s", msg.To, traceOut.From)
	}
	if traceOut.To != msg.From {
		t.Fatalf("Expected trace To to be %s; got %s", msg.From, traceOut.To)
	}

}

func TestTracerNonBlockingMode(t *testing.T) {
	ep := usrv.Endpoint{
		Name: "traceTest",
		Handler: usrv.HandlerFunc(func(ctx context.Context, rw usrv.ResponseWriter, req *usrv.Message) {
			rw.WriteError(errors.New("I cannot allow you to do that Dave"))
		}),
	}

	var err error

	traceChan := make(chan TraceEntry)
	err = Tracer(traceChan)(&ep)
	if err != nil {
		t.Fatalf("Error applying Tracer() to endpoint: %v", err)
	}

	msg := &usrv.Message{
		From:          "sender",
		To:            "recipient",
		CorrelationId: "123",
	}

	// Send request
	done := make(chan struct{})
	go func() {
		w := usrvtest.NewRecorder()
		ep.Handler.Serve(context.Background(), w, msg)

		done <- struct{}{}
	}()

	// We used a non-buffered channel so that the middleware will block as
	// noone is reading from it. We expect the middleware to drop the log
	select {
	case <-done:
	case <-time.After(time.Second * 1):
		t.Fatalf("Expected Tracer() middleware to drop logs as traceChan cannot be written to without blocking")
	}
}

func TestTraceSorting(t *testing.T) {
	now := time.Now()

	type spec struct {
		input    Trace
		expected Trace
	}

	testCases := []spec{
		{
			input: Trace{
				TraceEntry{Type: Request, Timestamp: now.Add(time.Second * 2)},
				TraceEntry{Type: Response, Timestamp: now.Add(time.Second)},
				TraceEntry{Type: Request, Timestamp: now},
			},
			expected: Trace{
				TraceEntry{Type: Request, Timestamp: now},
				TraceEntry{Type: Response, Timestamp: now.Add(time.Second)},
				TraceEntry{Type: Request, Timestamp: now.Add(time.Second * 2)},
			},
		},
		{
			input: Trace{
				TraceEntry{Type: Response, Timestamp: now},
				TraceEntry{Type: Request, Timestamp: now},
			},
			expected: Trace{
				TraceEntry{Type: Request, Timestamp: now},
				TraceEntry{Type: Response, Timestamp: now},
			},
		},
		{
			input: Trace{
				TraceEntry{Type: Request, Timestamp: now},
				TraceEntry{Type: Response, Timestamp: now},
			},
			expected: Trace{
				TraceEntry{Type: Request, Timestamp: now},
				TraceEntry{Type: Response, Timestamp: now},
			},
		},
		{
			input: Trace{
				TraceEntry{Type: Request, Timestamp: now},
				TraceEntry{Type: Response, Timestamp: now.Add(time.Second)},
			},
			expected: Trace{
				TraceEntry{Type: Request, Timestamp: now},
				TraceEntry{Type: Response, Timestamp: now.Add(time.Second)},
			},
		},
	}

	for index, testCase := range testCases {
		sort.Sort(testCase.input)
		if !reflect.DeepEqual(testCase.expected, testCase.input) {
			t.Fatalf("[case %d] expected sort output to be %v; got %v", index, testCase.expected, testCase.input)
		}
	}
}
