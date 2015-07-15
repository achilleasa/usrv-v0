# usrv
[![Build Status](https://drone.io/github.com/achilleasa/usrv/status.png)](https://drone.io/github.com/achilleasa/usrv/latest)
[![codecov.io](http://codecov.io/github/achilleasa/usrv/coverage.svg?branch=master)](http://codecov.io/github/achilleasa/usrv?branch=master)

A microservice framework for go

# Dependencies

To use usrv you need the following dependencies:
```
go get golang.org/x/net/context
go get github.com/achilleasa/usrv-service-adapters
```

# The goal of this framework

The goal of this framework is to provide a low-level api for creating microservices. It is not meant to be used
stand-alone. In fact, it doesn't even support any sort of serialization mechanism
(all messages work with byte slice payloads).

It should be used as part of a microservice platform library that builds on top of usrv to provide message serialization
and service discovery mechanisms.

# Transports

The core of this framework is the notion of a transport. The transport is responsible for binding to a service endpoint
(either as a server or a client) and handle the flow of messages from and to the endpoint.

Usrv ships with an amqp transport implementation as well as an [in-memory transport](https://github.com/achilleasa/usrv/blob/master/usrvtest/tansport.go)
that can be used for running tests.

The framework provides a common abstraction to the actual low-level message types used by each transport making it easier to
add new transports and use them as drop-in replacements for existing transports.

## AMQP transport

The AMQP transport uses rabbitmq as the transport backend. Given a service endpoint name, all servers
bind to the same queue name making it easy to horizontally scale your service by launching more instances. Each client
allocates a private queue which is then used as a reply channel for outgoing messages.

# The server

## Server options

When invoking the `NewServer` method to create a new server you may specify zero or more server options. The following options are supported:

| Option name     | Description
|-----------------|-----------------
| WithLogger      | Attach a specific `log.Logger` instance to the server. By default a NULL logger is used
| EventListener   | Register a channel for receiving server [events](#server-events).

## Server events

The server will emit various events while running. You can register a listener for those events using the
`EventListener` server option. The table below summarizes the list of events that the server reports.
EvtRegistered     EventType = iota // Registered endpoint
	EvtStarted                  = iota // Dialed transport
	EvtServing                  = iota // Serving endpoint
	EvtStopping                 = iota // Stopping server
	EvtStopped                  = iota // Server stopped and requests drained
	EvtTransportReset           = iota // Transport connection reset

| Event name         | Event includes endpoint name? | Description
|--------------------|-------------------------------|-----------------
| EvtRegistered      | yes                           | Registered endpoint
| EvtStarted         | no                            | Dialed transport
| EvtServing         | yes                           | Serving endpoint
| EvtStopping        | no                            | Stopping server
| EvtStopped         | no                            | Server stopped and requests drained
| EvtTransportReset  | no                            | Transport connection reset

## Request handlers

usrv request handlers are modeled after the handlers from the built-in go http package. The framework defines the
[Handler](https://github.com/achilleasa/usrv/blob/master/handler.go) interface that should be implemented by request
handlers and the `HandlerFunc` helper that allows you to wrap ordinary functions with a Handler-compatible signature.

The request handlers receive as arguments:
 - a [context](https://godoc.org/golang.org/x/net/context) object which may be used for handling cancellations and timeouts.
 - a [ResponseWriter](https://github.com/achilleasa/usrv/blob/master/transport.go#L59) implementation for the transport in use. The handler should respond to the incoming request (with a message or with an error) using the response writer.
 - a [Message](https://github.com/achilleasa/usrv/blob/master/transport.go#L30) instance that wraps the received low-level transport message.


To register a request handler you need to use the `Handle` method of the `Server` object. The method receives as arguments:
 - the endpoint to serve.
 - the handler for incoming requests.
 - zero or more [middleware](#middleware) to be applied.

### Middleware

Request middleware are built using the functional argument pattern. The serve as generators, returning a method to be
invoked with a pointer to an [Endpoint](https://github.com/achilleasa/usrv/blob/master/endpoint.go#L7). The following middleware are available:

| Middleware name | Description
|-----------------|-----------------
| Throttle        | Limit the max number of concurrent requests (maxConcurrent param). In addition, an execution timeout (as a time.Duration object) may be specified. If the request cannot be served within the specified deadline, it will be automatically aborted with a `ErrTimeout` error.


## Listening and processing requests

After defining the endpoint handlers for your services, you can instruct the server to bind to them and begin processing requests by invoking the `ListenAndServe` method. This method blocks until the server is shutdown so its best to spawn a go-routine and invoke `ListenAndServe` inside it.

## Shutting down the server

You can trigger a graceful server shutdown by invoking the `Close` method. When this method is invoked, any pending
requests will be aborted with a `ErrTimeout` error. 

The server will unbind the service endpoint so no further
requests are received and then the method will block until all pending requests have been flushed.


# The client

The client provides a Future-based API for performing asynchronous RPC calls. 

When a new request is made, the client
will assign a unique correlation id to it, add it to a list of pending requests and return a read-only channel where the 
caller can block waiting for the response to arrive.

The client uses a background worker to match incoming server responses to pending requests and emits the response
to the channel allocated by the matched request.

The client provides two methods for executing requests `Request` and `RequestWithTimeout`. Both methods receive a `context`
as their first argument. The `RequestWithTimeout` method also allows you to specify a timeout for the request. If the timeout
expires before a response is received, the client will automatically expire the request with an `ErrTimeout` error.

Both request methods return a `<- chan ServerReponse` to the caller. The [ServerResponse](https://github.com/achilleasa/usrv/blob/master/client.go#L32) struct contains a `Message` and an `Error`. If an error occurs, `Message` will be `nil` and the `Error` will contain the error that occured.

# Examples

Coming soon...

# Testing

Coming soon...

# License

usrv is distributed under the [MIT license](https://github.com/achilleasa/usrv/blob/master/LICENSE).

