package usrvtest

import (
	"errors"
	"log"
	"time"

	"sync"

	"code.google.com/p/go-uuid/uuid"
	"github.com/achilleasa/service-adapters"
	"github.com/achilleasa/usrv"
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
	sync.Mutex
	closeListeners *adapters.Notifier
	bindings       map[string]*usrv.Binding
	failMask       FailMask
	connected      bool
}

func NewTransport() *InMemoryTransport {
	return &InMemoryTransport{
		bindings:       make(map[string]*usrv.Binding),
		closeListeners: adapters.NewNotifier(),
	}
}

// Set a fail mask. Depending on which bits are set, transport operations will fail.
func (t *InMemoryTransport) SetFailMask(mask FailMask) {
	t.failMask = mask
}

// Connect to the transport.
func (t *InMemoryTransport) Dial() error {
	t.Lock()
	defer t.Unlock()

	if t.connected {
		return nil
	}

	if (t.failMask & FailDial) == FailDial {
		return usrv.ErrDialFailed
	}

	t.connected = true
	return nil
}

func (t *InMemoryTransport) Close() {
	t.Lock()
	defer t.Unlock()

	if !t.connected {
		return
	}

	for _, binding := range t.bindings {
		close(binding.Messages)
	}

	t.closeListeners.NotifyAll(usrv.ErrClosed)

	// Clear all listeners and bindings
	t.connected = false
	t.bindings = make(map[string]*usrv.Binding)
}

// Bind an endpoint to the transport.
func (t *InMemoryTransport) Bind(bindingType usrv.BindingType, endpoint string) (*usrv.Binding, error) {
	t.Lock()
	defer t.Unlock()

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

	t.bindings[binding.Name] = binding
	cnt := 0
	for _ = range t.bindings {
		cnt++
	}
	return binding, nil
}

// Send a message.
func (t *InMemoryTransport) Send(msg *usrv.Message) error {
	if (t.failMask & FailSend) == FailSend {
		return errors.New("Send failed")
	}

	// Forward to proper address
	binding, exists := t.bindings[msg.To]
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

	binding.Messages <- usrv.TransportMessage{
		ResponseWriter: resWriter,
		Message:        msg,
	}

	return nil
}

func (t *InMemoryTransport) NotifyClose(c chan error) {
	t.closeListeners.Add(c)
}

// Simulate a connection reset.
func (t *InMemoryTransport) Reset() {
	t.Lock()
	defer t.Unlock()

	for _, binding := range t.bindings {
		close(binding.Messages)
	}

	// Close all listeners to indicate a reset
	t.closeListeners.NotifyAll(nil)

	// Clear all listeners and bindings
	t.bindings = make(map[string]*usrv.Binding)
	t.connected = false
}

// Set logger.
func (t *InMemoryTransport) SetLogger(logger *log.Logger) {
	// no-op
}
