package usrv

import (
	"golang.org/x/net/context"
)

// Objects implementing the Handler interface can be
// registered to serve a microservice endpoint
type Handler interface {
	Serve(context.Context, ResponseWriter, *Message)
}

// The HandlerFunc type is an adapter to allow the use of
// ordinary functions as microservice handlers.  If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// Handler object that calls f.
type HandlerFunc func(context.Context, ResponseWriter, *Message)

// Serve implementation delegates the call to the wrapped function
func (f HandlerFunc) Serve(ctx context.Context, writer ResponseWriter, req *Message) {
	f(ctx, writer, req)
}
