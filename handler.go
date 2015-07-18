package usrv

import "golang.org/x/net/context"

// Objects implementing the Handler interface can be
// registered as RPC request handlers.
type Handler interface {
	Serve(context.Context, ResponseWriter, *Message)
}

// The HandlerFunc type is an adapter to allow the use of
// ordinary functions as RPC handlers.  If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// Handler object that calls f.
type HandlerFunc func(context.Context, ResponseWriter, *Message)

// Serve implementation delegates the call to the wrapped function.
func (f HandlerFunc) Serve(ctx context.Context, writer ResponseWriter, req *Message) {
	f(ctx, writer, req)
}

// This handler provides an adapter for using a message serialization framework (e.g protobuf, msgpack, json)
// with usrv. It applies a 3-stage pipeline to the incoming raw request:
//
// raw payload -> decoder -> process -> encode -> write to response writer
type PipelineHandler struct {
	Decoder   func([]byte) (interface{}, error)
	Processor func(context.Context, interface{}) (interface{}, error)
	Encoder   func(interface{}) ([]byte, error)
}

// Implementation of the Handler interface
func (f PipelineHandler) Serve(ctx context.Context, writer ResponseWriter, reqMsg *Message) {
	// Unmarshal request
	request, err := f.Decoder(reqMsg.Payload)
	if err != nil {
		writer.WriteError(err)
		return
	}

	// Pass to processor
	res, err := f.Processor(ctx, request)
	if err != nil {
		writer.WriteError(err)
		return
	}

	// Marshal response
	payload, err := f.Encoder(res)
	if err != nil {
		writer.WriteError(err)
		return
	}
	writer.Write(payload)
}
