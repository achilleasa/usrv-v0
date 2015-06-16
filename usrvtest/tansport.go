package usrvtest

import (
	"errors"
	"time"

	"code.google.com/p/go-uuid/uuid"
	"github.com/achilleasa/usrv"
	"golang.org/x/net/context"
)

type FailMask int

const (
	FailDial FailMask = 1 << iota
	FailBind          = 1 << iota
	FailSend          = 1 << iota
)

// InMemoryTransport is an implementation of usrv.Transport that
// uses ResponseRecorder as its ResponseWriter implementation
type InMemoryTransport struct {
	channels map[string]chan usrv.TransportMessage
	bindings map[string]*usrv.Binding
	failMask FailMask
}

func NewTransport() *InMemoryTransport {
	return &InMemoryTransport{
		channels: make(map[string]chan usrv.TransportMessage),
		bindings: make(map[string]*usrv.Binding),
	}
}

// Set a fail mask. Depending on which bits are set, transport operations will fail.
func (t *InMemoryTransport) SetFailMask(mask FailMask) {
	t.failMask = mask
}

// Connect to the transport.
func (t *InMemoryTransport) Dial() error {
	if (t.failMask & FailDial) == FailDial {
		return errors.New("Dial failed")
	}
	return nil
}

// Bind an endpoint to the transport. The implementation should monitor the passed
// context and terminate the binding once the context is cancelled.
func (t *InMemoryTransport) Bind(ctx context.Context, bindingType usrv.BindingType, endpoint string) (*usrv.Binding, error) {
	if (t.failMask & FailBind) == FailBind {
		return nil, errors.New("Bind failed")
	}

	var binding *usrv.Binding

	// Use a buffered channel so tests do not block
	msgChan := make(chan usrv.TransportMessage, 1)

	if bindingType == usrv.ServerBinding {
		binding = &usrv.Binding{
			Type:     bindingType,
			Name:     endpoint,
			Messages: msgChan,
		}
	} else {
		// Generate a random name for the client binding
		binding = &usrv.Binding{
			Type:     bindingType,
			Name:     uuid.New(),
			Messages: msgChan,
		}
	}

	t.channels[binding.Name] = msgChan
	t.bindings[binding.Name] = binding

	return binding, nil
}

// Send a message.
func (t *InMemoryTransport) Send(msg *usrv.Message) error {
	if (t.failMask & FailSend) == FailSend {
		return errors.New("Send failed")
	}

	// Forward to proper address
	msgChan, exists := t.channels[msg.To]
	if !exists {
		return errors.New("Unknown destination endpoint")
	}

	// Setup response writer if this is a server endpoint
	var resWriter *ResponseRecorder
	if t.bindings[msg.To].Type == usrv.ServerBinding {
		resWriter = NewRecorder(
			WithTransport(t),
			WithMessage(&usrv.Message{
				Headers:       make(usrv.Header),
				From:          msg.To,      // this endpoint
				To:            msg.ReplyTo, // the reply endpoint of the sender
				CorrelationId: msg.CorrelationId,
				Timestamp:     time.Now(),
			}),
		)
	}

	msgChan <- usrv.TransportMessage{
		ResponseWriter: resWriter,
		Message:        msg,
	}

	return nil
}
