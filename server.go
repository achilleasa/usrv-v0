package usrv

import (
	"errors"
	"log"
	"os"
	"sync"

	"golang.org/x/net/context"
)

// Errors introduced by RPC server.
var (
	ErrCancelled    = errors.New("Request was cancelled")
	ErrTimeout      = errors.New("Request timeout")
	ErrResponseSent = errors.New("Response already sent")
)

// Context keys.
const (
	CtxCurEndpoint = "curEndpoint"
	CtxTraceId     = "traceId"
)

// An RPC server implementation.
type Server struct {

	// The root server context.
	ctx context.Context

	// A context-provided method for shutting down pending requests.
	cancel context.CancelFunc

	// The list of registered endpoints.
	endpoints []Endpoint

	// The logger for server messages.
	Logger *log.Logger

	// The transport used for handling requests.
	transport Transport

	// This waitgroup tracks ongoing requests so we can
	// properly drain them before terminating the server.
	pending sync.WaitGroup
}

// Create a new server with default settings. One or more ServerOption functional arguments
// may be specified to override defaults.
func NewServer(transport Transport, options ...ServerOption) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create server with default settings
	server := &Server{
		ctx:       ctx,
		cancel:    cancel,
		endpoints: make([]Endpoint, 0),
		transport: transport,
		Logger:    log.New(os.Stderr, "", log.LstdFlags),
	}

	return server, server.SetOption(options...)
}

// Set one or more ServerOptions.
func (srv *Server) SetOption(options ...ServerOption) error {
	// Apply any options
	for _, opt := range options {
		if err := opt(srv); err != nil {
			return err
		}
	}

	return nil
}

// Register a handler for requests to RPC endpoint identified by path. One
// or more EndpointOption functional arguments may be specified to further
// customize the endpoint by applying for example middleware.
func (srv *Server) Handle(path string, handler Handler, options ...EndpointOption) {
	ep := Endpoint{path, handler}

	// Apply any options
	for _, opt := range options {
		opt(&ep)
	}

	// Append to endpoint list
	srv.endpoints = append(srv.endpoints, ep)
}

// Dial the registered transport provider and then call Serve()
// to start processing incoming RPC requests.
func (srv *Server) ListenAndServe() error {
	var err error

	// Connect to transport
	err = srv.transport.Dial()
	if err != nil {
		return err
	}

	return srv.Serve()
}

// Bind each defined endpoint to the transport and start processing incoming requests.
// This function will block until Close() is invoked.
func (srv *Server) Serve() error {
	for _, ep := range srv.endpoints {

		binding, err := srv.transport.Bind(srv.ctx, ServerBinding, ep.Name)
		if err != nil {
			srv.Close()
			return err
		}

		go func(queue <-chan TransportMessage, ep Endpoint) {
			srv.serveEndpoint(queue, &ep)
		}(binding.Messages, ep)
	}

	// Block till our context is cancelled
	<-srv.ctx.Done()

	return nil
}

// Implement a message processing loop for a bound endpoint. A separate go-routine will
// be spawned for each incoming message. This function will block until Close() is invoked.
func (srv *Server) serveEndpoint(incoming <-chan TransportMessage, ep *Endpoint) {
	srv.Logger.Printf("Serving requests to endpoint %s\n", ep.Name)

	for {
		select {
		case trMsg, ok := <-incoming:
			if !ok {
				incoming = nil
				continue
			}
			srv.pending.Add(1)
			go func() {
				defer srv.pending.Done()

				// Create a new context with our endpoint name
				rctx := context.WithValue(srv.ctx, CtxCurEndpoint, ep.Name)

				// Serve the request and flush the response writer
				ep.Handler.Serve(rctx, trMsg.ResponseWriter, trMsg.Message)

				err := trMsg.ResponseWriter.Close()
				if err != nil {
					panic(err)
				}

			}()
		case <-srv.ctx.Done():
			return
		}
	}
}

// Shutdown the server. This function will unbind any bound endpoints and block until
// any pending requests have been drained.
func (srv *Server) Close() {

	srv.Logger.Printf("Server shutdown in progress; waiting for pending requests to drain\n")

	// Stop processing further requests and shutdown transport
	srv.cancel()

	// Drain pending requests
	srv.pending.Wait()
	srv.Logger.Printf("Server shutdown complete\n")
}
