package transport

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"sync"

	"github.com/achilleasa/usrv"
	"github.com/achilleasa/usrv-service-adapters"
	amqpAdapter "github.com/achilleasa/usrv-service-adapters/service/amqp"
	amqpDrv "github.com/streadway/amqp"
)

// We need to repeat this error so as not to create a dependency loop with usrv
var (
	errServiceUnavailable = errors.New("Service not available")
)

// Amqp is a singleton transport instance that works with the amqp service adapter
var Amqp *amqp = &amqp{
	srvAdapter: amqpAdapter.Adapter,
	logger:     log.New(ioutil.Discard, "", log.LstdFlags),
}

// The Amqp type represents a transport service that can talk to AMQP servers (e.g. rabbitmq)
type amqp struct {

	// A mutex that guards access to the struct fields.
	sync.Mutex

	// The amqp connection adapter
	srvAdapter *amqpAdapter.Amqp

	// The logger for server messages.
	logger *log.Logger

	// AMQP channel handle.
	channel *amqpDrv.Channel

	// A notifier for close events.
	closeNotifier *adapters.Notifier
}

// Connect to the AMQP server.
func (a *amqp) Dial() error {
	a.Lock()
	defer a.Unlock()
	err := a.srvAdapter.Dial()
	if err != nil {
		return usrv.ErrDialFailed
	}

	// Allocate channel
	a.channel, err = a.srvAdapter.NewChannel()
	if err != nil {
		return err
	}

	return nil
}

// Close the transport.
func (a *amqp) Close() {
	a.srvAdapter.Close()
	a.channel = nil
}

// Bind a client or server endpoint to the AMQP server.
//
// For server bindings we use the endpoint name  as a queue to listen to for messages. For
// client bindings a private amqp queue will be allocated so the server can route RPC responses
// to this particular client.
//
func (a *amqp) Bind(bindingType usrv.BindingType, endpoint string) (*usrv.Binding, error) {
	var queue amqpDrv.Queue
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

	a.logger.Printf("[TRANSPORT] Declared queue %s for endpoint: %s\n", queue.Name, endpoint)

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

	// Register listener for undelivered messages if this is a client binding
	returns := make(chan amqpDrv.Return)
	if bindingType == usrv.ClientBinding {
		returns = a.channel.NotifyReturn(returns)
	}

	msgChan := make(chan usrv.TransportMessage)
	go func() {
		for {
			select {
			case r, ok := <-returns:
				// Channel closed; exit worker
				if !ok {
					a.logger.Printf("[TRANSPORT] Shutting down listener for endpoint %s\n", endpoint)
					return
				}

				// Emit a transport message to the msg channel notifying that message delivery failed
				if r.Headers == nil {
					r.Headers = amqpDrv.Table{}
				}
				headers := usrv.Header(r.Headers)
				headers["error"] = errServiceUnavailable.Error()
				msgChan <- usrv.TransportMessage{
					ResponseWriter: nil,

					Message: &usrv.Message{
						Headers:       headers,
						From:          endpoint,
						To:            r.AppId,
						CorrelationId: r.CorrelationId,
						Timestamp:     r.Timestamp,
						Payload:       nil,
					},
				}

			case d, ok := <-deliveries:

				// Channel closed; exit worker
				if !ok {
					a.logger.Printf("[TRANSPORT] Shutting down listener for endpoint %s\n", endpoint)
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
func (a *amqp) Send(msg *usrv.Message) error {
	err := a.channel.Publish(
		"",
		msg.To,
		true, // the server should immediately report undelived messages
		false,
		amqpDrv.Publishing{
			Headers:       amqpDrv.Table(msg.Headers),
			CorrelationId: msg.CorrelationId,
			AppId:         msg.From,
			Timestamp:     msg.Timestamp,
			Body:          msg.Payload,
			ReplyTo:       msg.ReplyTo,
		},
	)

	return err
}

// Register a listener for receiving close notifications. The transport will emit an error and
// close the channel if the transport is cleanly shut down or close the channel if the connection is reset.
func (a *amqp) NotifyClose(c chan error) {
	a.srvAdapter.NotifyClose(c)
}

// Set logger.
func (a *amqp) SetLogger(logger *log.Logger) {
	a.logger = logger
}
