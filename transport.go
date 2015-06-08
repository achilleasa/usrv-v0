package usrv

import (
	"golang.org/x/net/context"
	"io"
	"time"
)

type BindingType int

const (
	ServerBinding BindingType = iota
	ClientBinding
)

type Header map[string]interface{}

type Message struct {
	Headers       Header
	From          string
	To            string
	CorrelationId string
	ReplyTo       string
	Timestamp     time.Time
	Payload       []byte
}

type ResponseWriter interface {
	Header() Header

	WriteError(err error) error

	WriteHeader(name string, value interface{})

	io.WriteCloser
}

type TransportMessage struct {
	ResponseWriter ResponseWriter
	Message        *Message
}

type Binding struct {
	Type     BindingType
	Name     string
	Messages <-chan TransportMessage
}

type Transport interface {
	// Connect to the transport
	Dial() error

	// Bind an endpoint to the transport and return a channel for processing incoming requests.
	Bind(ctx context.Context, bindingType BindingType, endpoint string) (*Binding, error)

	// Send a message
	Send(msg *Message) error
}
