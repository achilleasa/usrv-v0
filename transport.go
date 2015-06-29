package usrv

import "time"

type BindingType int

// The supported types of endpoint bindings.
const (
	ServerBinding BindingType = iota
	ClientBinding
)

type Header map[string]interface{}

// Get a key from the header map. Returns nil if key does not exist
func (h Header) Get(key string) interface{} {
	return h[key]
}

// Set a key to the header map
func (h Header) Set(key string, value interface{}) {
	h[key] = value
}

// The Message object provides a wrapper for the actual message implementation
// used by each transport provider.
type Message struct {
	// A map of headers.
	Headers Header

	// The sender endpoint
	From string

	// The recipient endpoint
	To string

	// A unique identifier used by clients to match requests to incoming
	// responses when concurrent requests are in progress.
	CorrelationId string

	// A reply address for routing responses.
	ReplyTo string

	// The message generation timestamp.
	Timestamp time.Time

	// The message content.
	Payload []byte
}

// Objects implementing the ResponseWriter interface can be used
// for responding to incoming RPC messages.
//
// Since the actual reply serialization is transport-specific,
// each transport should define its own implementation.
type ResponseWriter interface {

	// Get back the headers that will be sent with the response payload.
	// Modifying headers after invoking Write (or WriteError) has no effect.
	Header() Header

	// Write an error to the response.
	WriteError(err error) error

	// Write the data to the response and return the number of bytes that were actually written.
	Write(data []byte) (int, error)

	// Flush the written data and headers and close the ResponseWriter. Repeated invocations
	// of Close() should fail with ErrResponseSent.
	Close() error
}

// Transports generate TransportMessage objects for incoming messages
// at each bound endpoint.
type TransportMessage struct {
	// A transport-specific ResponseWriter implementation for replying to the incoming message.
	ResponseWriter ResponseWriter

	// The incoming message payload and its metadata.
	Message *Message
}

type Binding struct {
	// The type of the binding.
	Type BindingType

	// The name of the binding. If this is a server binding then Name should match the endpoint name.
	Name string

	// A channel for incoming messages.
	Messages chan TransportMessage
}

// Objects implementing the Transport interface can be used
// by both the Server and Client as message transports.
type Transport interface {
	// Set the dial policy for this transport.
	SetDialPolicy(policy DialPolicy)

	// Connect to the transport. If a dial policy has been specified,
	// the transport will keep trying to reconnect until a connection
	// is established or the dial policy aborts the reconnection attempt.
	Dial() error

	// Disconnect.
	Close()

	// Bind an endpoint to the transport. The implementation should monitor the passed
	// context and terminate the binding once the context is cancelled.
	Bind(bindingType BindingType, endpoint string) (*Binding, error)

	// Send a message.
	Send(msg *Message) error

	// Register a listener for receiving close notifications. The transport will emit an error and
	// close the channel if the transport is cleanly shut down or close the channel if the connection is reset.
	NotifyClose(c chan error)
}
