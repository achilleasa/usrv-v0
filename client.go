package usrv

import (
	"errors"
	"sync"

	"time"

	"code.google.com/p/go-uuid/uuid"
	"golang.org/x/net/context"
)

// This structure models a server response to an outgoing client request.
type ServerResponse struct {
	// The server response message.
	Message *Message

	// An error reported by the remote server. It will be nil if no error was reported.
	Error error
}

// An structure for enqueuing request jobs to the background worker.
type job struct {
	msg     *Message
	resChan chan ServerResponse
}

type Client struct {

	// The client root context.
	ctx context.Context

	// A context-provided method for shutting down pending requests.
	cancel context.CancelFunc

	// The transport used for handling requests.
	transport Transport

	// The client transport binding.
	binding *Binding

	// The pending group tracks ongoing requests so we can
	// properly drain them before terminating the client.
	pending sync.WaitGroup

	// pendingMap maps pending Correlation Ids to the channel used for the received response.
	pendingMap map[string]chan ServerResponse

	// A channel for submitting requests to the background worker.
	jobChan chan job

	// A channel for cancelling ongoing requests by sending their correlationIds
	cancelChan chan string

	endpoint string
}

// Create a new client for the given endpoint.
func NewClient(transport Transport, endpoint string) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create server with default settings
	client := &Client{
		ctx:        ctx,
		cancel:     cancel,
		transport:  transport,
		pendingMap: make(map[string]chan ServerResponse),
		jobChan:    make(chan job),
		cancelChan: make(chan string),
		endpoint:   endpoint,
	}

	var err error
	err = client.transport.Dial()
	if err != nil {
		return nil, err
	}

	// Bind to the endpoint
	client.binding, err = client.transport.Bind(ctx, ClientBinding, endpoint)

	if err != nil {
		return nil, err
	}

	// Spawn worker
	go client.worker()

	return client, nil
}

// A background worker that processes incoming server responses, matches them to pending
// requests and routes the response to the appropriate channel so it may be consumed.
func (client *Client) worker() {
	for {
		select {
		case <-client.ctx.Done():

			// Fail any pending requests with a timeout error
			for _, resChan := range client.pendingMap {
				resChan <- ServerResponse{
					nil,
					ErrTimeout,
				}
				close(resChan)
			}

			// exit worker
			return
		case transportMsg := <-client.binding.Messages:
			// Try to match the correlation id to a pending request.
			// If we cannot find a match, ignore the response
			resChan, exists := client.pendingMap[transportMsg.Message.CorrelationId]
			if !exists {
				continue
			}

			// Remove from pending requests send the response and close the channel
			delete(client.pendingMap, transportMsg.Message.CorrelationId)

			var err error
			if errMsg, exists := transportMsg.Message.Headers["error"]; exists {
				err = errors.New(errMsg.(string))
			}

			resChan <- ServerResponse{
				transportMsg.Message,
				err,
			}
			close(resChan)

		case req := <-client.jobChan:

			// Send request. If we get an error while sending fail the request immediately
			err := client.transport.Send(req.msg)
			if err != nil {
				req.resChan <- ServerResponse{
					nil,
					err,
				}
				close(req.resChan)
				return
			}

			// Add the reply channel to the pendingMap using the correlationId
			// as the key so we can match the response later on.
			client.pendingMap[req.msg.CorrelationId] = req.resChan

		case correlationId := <-client.cancelChan:
			// Received a cancellation for a pending request
			resChan, exists := client.pendingMap[correlationId]
			if !exists {
				continue
			}

			// Remove from pending requests send the error and close the channel
			delete(client.pendingMap, correlationId)

			resChan <- ServerResponse{
				nil,
				ErrCancelled,
			}
			close(resChan)

		}

	}
}

// Create a new request to the underlying endpoint. Returns a read-only channel that
// will emit a ServerResponse once it is received by the server.
//
// If ctx is cancelled while the request is in progress, the client will fail the
// request with ErrTimeout
func (client *Client) Request(ctx context.Context, msg *Message) <-chan ServerResponse {
	return client.RequestWithTimeout(ctx, msg, 0)
}

// Create a new request to the underlying endpoint with a client timeout. Returns a
// read-only channel that will emit a ServerResponse once it is received by the server.
//
// If the timeout expires or ctx is cancelled while the request is in progress, the client
// will fail the request with ErrTimeout
func (client *Client) RequestWithTimeout(ctx context.Context, msg *Message, timeout time.Duration) <-chan ServerResponse {

	// If a non-zero timeout is specified, wrap the supplied ctx with a context that times out
	if timeout != 0 {
		ctx, _ = context.WithTimeout(ctx, timeout)
	}

	if msg.Headers == nil {
		msg.Headers = make(Header)
	}

	// Check the supplied context for the presence of curEndpoint. If it exists
	// we will set it as the "From" field of the outgoing message
	curEndpoint := ctx.Value(CtxCurEndpoint)
	if curEndpoint != nil {
		msg.From = curEndpoint.(string)
	}

	// Check the supplied context for the present of a traceId. If found,
	// inject it into the outgoing request headers
	traceId := ctx.Value(CtxTraceId)
	if traceId != nil {
		msg.Headers[CtxTraceId] = traceId
	}

	// Assign the private queue name as the message reply target and
	// the target endpoint as the "To" field of the outgoing message.
	// Finally allocate a UUID for matching the async server reply
	// to this request.
	msg.ReplyTo = client.binding.Name
	msg.To = client.endpoint
	msg.CorrelationId = uuid.New()

	// Allocate a buffered channel for the response. We use a buffered channel to
	// ensure that our job queue does not block if the requester never reads from the
	// returned channel
	clientResChan := make(chan ServerResponse, 1)

	reqJob := job{
		msg,
		make(chan ServerResponse, 1),
	}

	// Submit to worker and start a go-routine to process job replies and timeouts
	client.jobChan <- reqJob
	go func(ctx context.Context, correlationId string, jobResChan chan ServerResponse, clientResChan chan ServerResponse) {
		select {
		case <-ctx.Done():
			// ctx was cancelled or timeout exceeded.
			clientResChan <- ServerResponse{
				nil,
				ctx.Err(),
			}

			// Send cancellation request to worker
			client.cancelChan <- correlationId
		case res := <-jobResChan:
			clientResChan <- res
		}

		close(clientResChan)
	}(ctx, msg.CorrelationId, reqJob.resChan, clientResChan)

	return clientResChan
}
