package usrvtest

import (
	"time"

	"github.com/achilleasa/usrv"
)

type RecorderOption func(rec *ResponseRecorder)

// Bind a transport to the recorder.
func WithTransport(transport usrv.Transport) RecorderOption {
	return func(rec *ResponseRecorder) {
		rec.Transport = transport
	}
}

// Bind a message to the recorder.
func WithMessage(msg *usrv.Message) RecorderOption {
	return func(rec *ResponseRecorder) {
		rec.Message = msg
	}
}

// ResponseRecorder is an implementation of usrv.ResponseWriter that
// records its mutations for later inspection in tests.
type ResponseRecorder struct {
	Message   *usrv.Message
	Transport usrv.Transport
	Flushed   bool
}

// Create a new recorder.
func NewRecorder(options ...RecorderOption) *ResponseRecorder {
	rec := &ResponseRecorder{
		Message: &usrv.Message{
			Headers:   make(usrv.Header),
			Timestamp: time.Now(),
		},
	}

	for _, opt := range options {
		opt(rec)
	}

	return rec
}

// Get header map.
func (w *ResponseRecorder) Header() usrv.Header {
	return w.Message.Headers
}

// Write an error.
func (w *ResponseRecorder) WriteError(err error) error {
	if w.Flushed {
		return usrv.ErrResponseSent
	}

	w.Message.Headers.Set("error", err.Error())

	return nil
}

// Write data payload.
func (w *ResponseRecorder) Write(data []byte) (int, error) {
	if w.Flushed {
		return 0, usrv.ErrResponseSent
	}

	w.Message.Payload = data

	return len(data), nil
}

// Flush the response.
func (w *ResponseRecorder) Close() error {
	if w.Flushed {
		return usrv.ErrResponseSent
	}

	w.Flushed = true
	if w.Transport != nil {
		w.Transport.Send(w.Message)
	}
	return nil
}
