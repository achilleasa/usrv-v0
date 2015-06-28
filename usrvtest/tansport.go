package usrvtest

import (
	"errors"
	"time"

	"sync"

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
	sync.Mutex
	closeListeners []chan error
	bindings       map[string]*usrv.Binding
	failMask       FailMask
	connected      bool
	dialPolicy     usrv.DialPolicy
}

func NewTransport() *InMemoryTransport {
	return &InMemoryTransport{
		closeListeners: make([]chan error, 0),
		bindings:       make(map[string]*usrv.Binding),
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

	if t.dialPolicy != nil {
		wait, err := t.dialPolicy.NextRetry()
		if err != nil {
			return usrv.ErrDialFailed
		}
		<-time.After(wait)
	}

	if (t.failMask & FailDial) == FailDial {
		return usrv.ErrDialFailed
	}

	t.connected = true
	if t.dialPolicy != nil {
		t.dialPolicy.ResetAttempts()
	}
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

	for _, listener := range t.closeListeners {
		listener <- usrv.ErrClosed
	}

	// Clear all listeners and bindings
	t.connected = false
	t.bindings = make(map[string]*usrv.Binding)
	t.closeListeners = make([]chan error, 0)
}

// Bind an endpoint to the transport. The implementation should monitor the passed
// context and terminate the binding once the context is cancelled.
func (t *InMemoryTransport) Bind(ctx context.Context, bindingType usrv.BindingType, endpoint string) (*usrv.Binding, error) {
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
	t.Lock()
	defer t.Unlock()

	t.closeListeners = append(t.closeListeners, c)
}

// Simulate a connection reset.
func (t *InMemoryTransport) Reset() {
	t.Lock()
	defer t.Unlock()

	for _, binding := range t.bindings {
		close(binding.Messages)
	}

	// Close all listeners to indicate a reset
	for _, listener := range t.closeListeners {
		close(listener)
	}

	// Clear all listeners and bindings
	t.bindings = make(map[string]*usrv.Binding)
	t.closeListeners = make([]chan error, 0)
	t.connected = false
}

func (t *InMemoryTransport) SetDialPolicy(policy usrv.DialPolicy) {
	t.dialPolicy = policy
}
