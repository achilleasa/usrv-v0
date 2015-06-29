package transport

import (
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"sync"

	"errors"

	"github.com/achilleasa/usrv"
	"github.com/streadway/amqp"
)

// The Amqp type represents a transport service that can talk to AMQP servers (e.g. rabbitmq)
type Amqp struct {

	// A mutex protecting dial attempts.
	sync.Mutex

	// The logger for server messages.
	logger *log.Logger

	// AMQP connection handle.
	conn *amqp.Connection

	// AMQP channel handle.
	channel *amqp.Channel

	// AMQP endpoint.
	amqpEndpoint string

	// Connection status.
	connected bool

	// The dial policy to use.
	dialPolicy usrv.DialPolicy

	// A list of listeners to be notified when the connection is lost.
	closeListeners []chan error
}

// Create a new Amqp transport that will connect to uri. A logger instance
// may also be specified if you require logging output from the transport.
func NewAmqp(amqpEndpoint string, logger *log.Logger) *Amqp {

	if logger == nil {
		logger = log.New(ioutil.Discard, "", log.LstdFlags)
	}

	return &Amqp{
		amqpEndpoint:   amqpEndpoint,
		logger:         logger,
		closeListeners: make([]chan error, 0),
		dialPolicy:     usrv.PeriodicPolicy(1, time.Millisecond),
	}
}

// Connect to the AMQP server.
func (a *Amqp) Dial() error {
	a.Lock()
	defer a.Unlock()

	// We are already connected; ignore call
	if a.connected {
		return nil
	}

	var err error
	var wait time.Duration
	wait, err = a.dialPolicy.NextRetry()
	for {
		a.logger.Printf("Connecting to AMQP endpoint %s; attempt %d", a.amqpEndpoint, a.dialPolicy.CurAttempt())
		a.conn, err = amqp.Dial(a.amqpEndpoint)
		if err == nil {
			break
		}

		wait, err = a.dialPolicy.NextRetry()
		if err != nil {
			a.logger.Printf("Could not connect to AMQP endpoint %s after %d attempt(s)\n", a.amqpEndpoint, a.dialPolicy.CurAttempt())
			return usrv.ErrDialFailed
		}
		fmt.Errorf("Could not connect to AMQP endpoint %s; retrying in %v\n", a.amqpEndpoint, wait)
		<-time.After(wait)
	}

	a.logger.Printf("Connected to AMQP endpoint %s\n", a.amqpEndpoint)

	// Allocate channel
	a.channel, err = a.conn.Channel()
	if err != nil {
		return fmt.Errorf("Error allocating AMQP channel: %s\n", err)
	}

	a.connected = true
	a.dialPolicy.ResetAttempts()

	go func() {
		amqpClose := make(chan *amqp.Error)
		a.conn.NotifyClose(amqpClose)

		err, normalShutdown := <-amqpClose
		a.Lock()
		defer a.Unlock()

		for _, listener := range a.closeListeners {
			if err != nil && normalShutdown {
				listener <- errors.New(err.Error())
			}
			close(listener)
		}
	}()

	return nil
}

// Close the transport.
func (a *Amqp) Close() {
	a.Lock()
	defer a.Unlock()

	if !a.connected {
		return
	}

	// Close connection and notify any registered listeners
	a.conn.Close()
	for _, listener := range a.closeListeners {
		listener <- usrv.ErrClosed
		close(listener)
	}

	// Cleanup
	a.conn = nil
	a.channel = nil
	a.connected = false
	a.closeListeners = make([]chan error, 0)
}

// Bind a client or server endpoint to the AMQP server.
//
// For server bindings we use the endpoint name  as a queue to listen to for messages. For
// client bindings a private amqp queue will be allocated so the server can route RPC responses
// to this particular client.
//
func (a *Amqp) Bind(bindingType usrv.BindingType, endpoint string) (*usrv.Binding, error) {
	var queue amqp.Queue
	var err error

	if bindingType == usrv.ServerBinding {
		// When running as a server we listen to the queue matching the endpoint name
		queue, err = a.channel.QueueDeclare(
			endpoint,
			false, // durable
			true,  // delete when unused
			false, // exclusive
			false, // noWait
			nil,   // args
		)
	} else {
		// When running as a client we let the server allocate a random exclusive queue for us
		queue, err = a.channel.QueueDeclare(
			"",
			false, // durable
			true,  // delete when unused
			false, // exclusive
			false, // noWait
			nil,   // args
		)
	}

	if err != nil {
		return nil, fmt.Errorf("Error declaring endpoint queue %s: %s\n", endpoint, err)
	}

	a.logger.Printf("Declared queue %s for endpoint: %s\n", queue.Name, endpoint)

	// Create queue consumer
	deliveries, err := a.channel.Consume(
		queue.Name, // name
		queue.Name, // consumerTag (use same as queue name)
		false,      // noAck
		false,      // exclusive
		false,      // noLocal
		false,      // noWait
		nil,        // arguments
	)

	if err != nil {
		return nil, fmt.Errorf("Error consuming from queue %s: %s\n", endpoint, err)
	}

	msgChan := make(chan usrv.TransportMessage)
	go func() {
		for {
			select {
			case d, ok := <-deliveries:

				// Channel closed; exit worker
				if !ok {
					a.logger.Printf("Shutting down listener for endpoint %s\n", endpoint)
					return
				}

				// Setup response writer if this is a server endpoint
				var resWriter *amqpResponseWriter
				if bindingType == usrv.ServerBinding {
					resWriter = &amqpResponseWriter{
						amqp: a,
						message: &usrv.Message{
							Headers:       make(usrv.Header),
							From:          endpoint,  // this endpoint
							To:            d.ReplyTo, // the reply endpoint of the sender
							CorrelationId: d.CorrelationId,
							Timestamp:     time.Now(),
						},
					}
				}

				// Emit a transport message to the msg channel
				msgChan <- usrv.TransportMessage{
					ResponseWriter: resWriter,
					Message: &usrv.Message{
						Headers:       usrv.Header(d.Headers),
						From:          d.AppId,
						To:            endpoint,
						CorrelationId: d.CorrelationId,
						Timestamp:     d.Timestamp,
						Payload:       d.Body,
					},
				}
			}
		}
	}()

	binding := &usrv.Binding{
		Type:     bindingType,
		Name:     queue.Name,
		Messages: msgChan,
	}

	return binding, nil
}

// Send a message.
func (a *Amqp) Send(msg *usrv.Message) error {
	err := a.channel.Publish(
		"",
		msg.To,
		false,
		false,
		amqp.Publishing{
			Headers:       amqp.Table(msg.Headers),
			CorrelationId: msg.CorrelationId,
			AppId:         msg.From,
			Timestamp:     msg.Timestamp,
			Body:          msg.Payload,
			ReplyTo:       msg.ReplyTo,
		},
	)

	return err
}

// Set the dial policy for the transport.
func (a *Amqp) SetDialPolicy(policy usrv.DialPolicy) {
	a.Lock()
	defer a.Unlock()

	a.dialPolicy = policy
}

// Register a listener for receiving close notifications. The transport will emit an error and
// close the channel if the transport is cleanly shut down or close the channel if the connection is reset.
func (a *Amqp) NotifyClose(c chan error) {
	a.Lock()
	defer a.Unlock()

	a.closeListeners = append(a.closeListeners, c)
}

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
