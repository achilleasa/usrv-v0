package usrv

import (
	"log"
	"sync"

	"io/ioutil"

	"golang.org/x/net/context"
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

	// The transport used for handling requests.
	transport Transport

	// This waitgroup tracks ongoing requests so we can
	// properly drain them before terminating the server.
	pending sync.WaitGroup

	// Server event listeners.
	eventListeners []chan ServerEvent

	// The logger for server messages.
	Logger *log.Logger
}

// Create a new server with default settings. One or more ServerOption functional arguments
// may be specified to override defaults.
func NewServer(transport Transport, options ...ServerOption) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create server with default settings
	server := &Server{
		ctx:            ctx,
		cancel:         cancel,
		endpoints:      make([]Endpoint, 0),
		transport:      transport,
		Logger:         log.New(ioutil.Discard, "", log.LstdFlags),
		eventListeners: make([]chan ServerEvent, 0),
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
func (srv *Server) Handle(path string, handler Handler, options ...EndpointOption) error {
	ep := Endpoint{path, handler}

	// Apply any options
	for _, opt := range options {
		err := opt(&ep)
		if err != nil {
			return err
		}
	}

	// Append to endpoint list
	srv.endpoints = append(srv.endpoints, ep)
	srv.emitEvent(EvtRegistered, ep.Name)

	return nil
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
	srv.emitEvent(EvtStarted, "")

	for _, ep := range srv.endpoints {

		binding, err := srv.transport.Bind(srv.ctx, ServerBinding, ep.Name)
		if err != nil {
			srv.Close()
			return err
		}

		go func(binding *Binding, ep Endpoint) {
			srv.serveEndpoint(binding, &ep)
		}(binding, ep)
	}

	// Block till our context is cancelled
	<-srv.ctx.Done()

	return nil
}

// Implement a message processing loop for a bound endpoint. A separate go-routine will
// be spawned for each incoming message. This function will block until Close() is invoked.
func (srv *Server) serveEndpoint(binding *Binding, ep *Endpoint) {
	srv.emitEvent(EvtServing, ep.Name)

	var waitingForRebind chan struct{}

	for {
		select {
		case trMsg, ok := <-binding.Messages:
			if !ok {
				srv.Logger.Printf("Lost transport binding for endpoint %s", ep.Name)
				binding.Messages = nil

				// To ensure that our loop does not block while we wait for
				// the binding to be restored, we will setup the closed waitingForRebind channel
				waitingForRebind = make(chan struct{})
				close(waitingForRebind)
				continue
			}

			if waitingForRebind != nil {
				waitingForRebind = nil
				srv.Logger.Printf("Re-established transport binding for endpoint %s", ep.Name)
			}

			// Spawn go-routing to handle request
			srv.pending.Add(1)
			go func() {
				defer srv.pending.Done()

				// Create a new context with our endpoint name
				rctx := context.WithValue(srv.ctx, CtxCurEndpoint, ep.Name)

				// Serve the request and flush the response writer
				ep.Handler.Serve(rctx, trMsg.ResponseWriter, trMsg.Message)

				err := trMsg.ResponseWriter.Close()
				if err != nil {
					srv.Logger.Printf("Error flushing response: %s", err.Error())
				}

			}()
		case <-srv.ctx.Done():
			return
		case <-waitingForRebind:
			// Dummy always matching case so that our select does not deadlock
			// if our binding goes away
		}
	}
}

// Shutdown the server. This function will unbind any bound endpoints and block until
// any pending requests have been drained.
func (srv *Server) Close() {

	srv.emitEvent(EvtStopping, "")
	srv.Logger.Printf("Server shutdown in progress; waiting for pending requests to drain\n")

	// Stop processing further requests and shutdown transport
	srv.cancel()

	// Drain pending requests
	srv.pending.Wait()

	srv.emitEvent(EvtStopped, "")
	srv.Logger.Printf("Server shutdown complete\n")
}

// Emit server event in non-blocking mode. If the event cannot be sent to a
// registered listener channel then that particular listener will be skipped.
func (srv *Server) emitEvent(evtType EventType, endpoint string) {
	event := ServerEvent{
		Type:     evtType,
		Endpoint: endpoint,
	}

	for _, listener := range srv.eventListeners {
		select {
		case listener <- event:
			// Wrote event
		default:
			// Skip
		}
	}
}
