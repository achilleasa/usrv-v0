package transport

import (
	"fmt"
	"github.com/achilleasa/usrv"
	"github.com/streadway/amqp"
	"golang.org/x/net/context"
	"io/ioutil"
	"log"
	"time"
)

type Amqp struct {

	// The logger for server messages
	logger *log.Logger

	// AMQP connection
	conn *amqp.Connection

	// AMQP channel
	channel *amqp.Channel

	// Connection URI
	uri string

	// connection status
	connected bool
}

func NewAmqp(uri string, logger *log.Logger) *Amqp {

	if logger == nil {
		logger = log.New(ioutil.Discard, "", log.LstdFlags)
	}

	return &Amqp{
		uri:    uri,
		logger: logger,
	}
}

func (a *Amqp) Dial() error {
	var err error

	// We are already connected; ignore call
	if a.connected {
		return nil
	}

	// Connect to amqp
	a.conn, err = amqp.Dial(a.uri)
	if err != nil {
		return fmt.Errorf("Error connecting to AMQP endpoint %s: %s\n", a.uri, err)
	}

	a.logger.Printf("Connected to AMQP endpoint %s\n", a.uri)

	// Allocate channel
	a.channel, err = a.conn.Channel()
	if err != nil {
		return fmt.Errorf("Error allocating AMQP channel: %s\n", err)
	}

	a.logger.Printf("Allocated channel\n")

	return nil
}

func (a *Amqp) Bind(ctx context.Context, bindingType usrv.BindingType, endpoint string) (*usrv.Binding, error) {
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

	// Spawn a go-routine to fetch amqp messages, transform them to request objects
	// and emit them to the allocated request channel. The go-routine will automatically
	// exit when our incoming scope is cancelled
	msgChan := make(chan usrv.TransportMessage)
	go func() {
		for {
			select {
			case d, ok := <-deliveries:

				// If the channel has been closed set it to nil so we never
				// try to read from it again
				if !ok {
					deliveries = nil
					continue
				}

				// Setup response writer if this is a server endpoint
				var resWriter *amqpResponseWriter
				if bindingType == usrv.ServerBinding {
					resWriter = &amqpResponseWriter{
						amqp: a,
						msg: &usrv.Message{
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
			case <-ctx.Done():
				a.logger.Printf("Shutting down listener for endpoint %s\n", endpoint)

				// Cleanup
				a.channel.Cancel(queue.Name, false)
				return
			}
		}
	}()

	binding := &usrv.Binding{
		bindingType,
		queue.Name,
		msgChan,
	}

	return binding, nil
}

// Send a message
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

type amqpResponseWriter struct {
	amqp *Amqp
	msg  *usrv.Message

	flushed bool
}

func (w *amqpResponseWriter) Header() usrv.Header {
	return w.msg.Headers
}

func (w *amqpResponseWriter) WriteHeader(name string, value interface{}) {
	w.msg.Headers[name] = value
}

func (w *amqpResponseWriter) WriteError(err error) error {
	if w.flushed {
		return usrv.ErrResponseSent
	}

	w.msg.Headers["error"] = err.Error()

	return nil
}

func (w *amqpResponseWriter) Write(data []byte) (int, error) {
	if w.flushed {
		return 0, usrv.ErrResponseSent
	}

	w.msg.Payload = data

	return len(data), nil
}

func (w *amqpResponseWriter) Close() error {
	if w.flushed {
		return usrv.ErrResponseSent
	}

	w.flushed = true

	return w.amqp.Send(w.msg)
}
