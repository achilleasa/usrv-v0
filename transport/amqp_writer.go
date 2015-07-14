package transport

import "github.com/achilleasa/usrv"

// The amqpResponseWriter provides an amqp-specific implementation of a ResponseWriter.
type amqpResponseWriter struct {
	// The amqp instance handle.
	amqp *Amqp

	// An allocated message for encoding the written data.
	message *usrv.Message

	// Indicates whether this response has been flushed.
	flushed bool
}

// Get back the headers that will be sent with the response payload.
// Modifying headers after invoking Write (or WriteError) has no effect.
func (w *amqpResponseWriter) Header() usrv.Header {
	return w.message.Headers
}

// Write an error to the response.
func (w *amqpResponseWriter) WriteError(err error) error {
	if w.flushed {
		return usrv.ErrResponseSent
	}

	w.message.Headers.Set("error", err.Error())

	return nil
}

// Write the data to the response and return the number of bytes that were actually written.
func (w *amqpResponseWriter) Write(data []byte) (int, error) {
	if w.flushed {
		return 0, usrv.ErrResponseSent
	}

	w.message.Payload = data

	return len(data), nil
}

// Flush the written data and headers and close the ResponseWriter. Repeated invocations
// of Close() should fail with ErrResponseSent.
func (w *amqpResponseWriter) Close() error {
	if w.flushed {
		return usrv.ErrResponseSent
	}

	w.flushed = true

	return w.amqp.Send(w.message)
}
