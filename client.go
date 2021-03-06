package usrv

import (
	"errors"
	"sync"

	"time"

	"code.google.com/p/go-uuid/uuid"
	"golang.org/x/net/context"
)

var (
	// A RW mutex for protecting access to the injectedCtxFields map.
	injectCtxFieldsMutex sync.RWMutex

	// A set of ctx field names that should be injected by clients as headers into outgoing messages
	// if present in the request ctx
	injectCtxFields = make(map[string]struct{})
)

// Add fieldName to the list of ctx fields that will be injected by clients as headers into outgoing messages
// if present in the request ctx.
func InjectCtxFieldToClients(ctxField string) {
	injectCtxFieldsMutex.Lock()
	defer injectCtxFieldsMutex.Unlock()

	injectCtxFields[ctxField] = struct{}{}
}

// This structure models a server response to an outgoing client request.
type ServerResponse struct {
	// The server response message.
	Message *Message

	// An error reported by the remote server. It will be nil if no error was reported.
	Error error
}

// An structure for enqueuing request jobs to the background worker.
type job struct {
	msg     Message
	resChan chan ServerResponse
}

type Client struct {

	// The client root context.
	ctx context.Context

	// A context-provided method for shutting down pending requests.
	cancel context.CancelFunc

	// The transport used for handling requests.
	transport Transport

	// A listener for transport close events.
	closeNotifChan chan error

	// The client transport binding.
	binding *Binding

	// The pending group tracks active go-routines so we can
	// properly drain them before terminating the client.
	pending sync.WaitGroup

	// pendingMap maps pending Correlation Ids to the channel used for the received response.
	pendingMap map[string]chan ServerResponse

	// A channel for submitting requests to the background worker.
	jobChan chan job

	// A channel for cancelling ongoing requests by sending their correlationIds.
	jobCancelChan chan string

	// The endpoint this client connects to.
	endpoint string
}

// Create a new client for the given endpoint.
func NewClient(transport Transport) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	// Create server with default settings
	client := &Client{
		ctx:           ctx,
		cancel:        cancel,
		transport:     transport,
		pendingMap:    make(map[string]chan ServerResponse),
		jobChan:       make(chan job),
		jobCancelChan: make(chan string),
	}

	// Spawn worker
	client.pending.Add(1)
	go client.worker()

	return client
}

// Dial the transport, bind the endpoint listener and spawn a worker for multiplexing requests.
func (client *Client) Dial() error {

	var err error
	err = client.transport.Dial()
	if err != nil {
		// Force a nil binding
		client.binding = &Binding{}
		return err
	}

	// Register transport close listener and then allocate a random endpoint for server responses
	client.closeNotifChan = make(chan error, 1)
	client.transport.NotifyClose(client.closeNotifChan)
	client.binding, err = client.transport.Bind(ClientBinding, "")
	if err != nil {
		// Force a nil binding
		client.binding = &Binding{}
		return err
	}

	return nil
}

// A background worker that processes incoming server responses, matches them to pending
// requests and routes the response to the appropriate channel so it may be consumed.
func (client *Client) worker() {
	var dialErr = client.Dial()

	defer func() {

		close(client.jobChan)
		close(client.jobCancelChan)
		client.jobChan = nil
		client.jobCancelChan = nil

		// Fail any pending requests with ErrClosed or a dial error
		var failReason = ErrClosed
		if dialErr != nil {
			failReason = dialErr
		}

		for _, resChan := range client.pendingMap {
			resChan <- ServerResponse{
				nil,
				failReason,
			}
			close(resChan)
		}

		client.pending.Done()
	}()

	for {
		select {
		case <-client.ctx.Done():
			return
		case _, normalShutdown := <-client.closeNotifChan:
			if normalShutdown {
				return
			}

			// We lost our connection. Redial endpoint
			dialErr = client.Dial()
		case transportMsg, ok := <-client.binding.Messages:
			if !ok {
				client.binding.Messages = nil
				continue
			}

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
				// Check for known error types
				if errMsg == ErrCancelled.Error() {
					err = ErrCancelled
				} else if errMsg == ErrTimeout.Error() {
					err = ErrTimeout
				} else if errMsg == ErrServiceUnavailable.Error() {
					err = ErrServiceUnavailable
				} else {
					err = errors.New(errMsg.(string))
				}
			}

			resChan <- ServerResponse{
				transportMsg.Message,
				err,
			}
			close(resChan)

		case req := <-client.jobChan:

			// Send request; fail immediately if we got a dial error or the send fails
			var err = dialErr
			if err == nil {
				req.msg.ReplyTo = client.binding.Name
				err = client.transport.Send(&req.msg)
			}
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

		case correlationId := <-client.jobCancelChan:

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

// Create a new request to the specified endpoint. Returns a read-only channel that
// will emit a ServerResponse once it is received by the server and the request
// correlationId.
//
// If ctx is cancelled while the request is in progress, the client will fail the
// request with ErrTimeout
func (client *Client) Request(ctx context.Context, msg *Message, endpoint string) (<-chan ServerResponse, string) {
	srvRes, correlationId := client.RequestWithTimeout(ctx, msg, 0, endpoint)
	return srvRes, correlationId
}

// Create a new request to the specified endpoint with a client timeout. Returns a
// read-only channel that will emit a ServerResponse once it is received by the server
// and the request correlationId.
//
// If the timeout expires or ctx is cancelled while the request is in progress, the client
// will fail the request with ErrTimeout
func (client *Client) RequestWithTimeout(ctx context.Context, msg *Message, timeout time.Duration, endpoint string) (<-chan ServerResponse, string) {

	// Allocate a buffered channel for the response. We use a buffered channel to
	// ensure that our job queue does not block if the requester never reads from the
	// returned channel
	clientResChan := make(chan ServerResponse, 1)

	if client.jobChan == nil {
		clientResChan <- ServerResponse{
			nil,
			ErrClosed,
		}
		return clientResChan, ""
	}

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

	injectCtxFieldsMutex.RLock()
	for ctxField, _ := range injectCtxFields {
		val := ctx.Value(ctxField)
		if val != nil {
			msg.Headers[ctxField] = val
		}
	}
	injectCtxFieldsMutex.RUnlock()

	// Assign the private queue name as the message reply target and
	// the target endpoint as the "To" field of the outgoing message.
	// Finally allocate a UUID for matching the async server reply
	// to this request.
	msg.To = endpoint
	msg.CorrelationId = uuid.New()

	reqJob := job{
		*msg,
		make(chan ServerResponse, 1),
	}

	// Submit to worker and start a go-routine to process job replies and timeouts
	client.jobChan <- reqJob
	client.pending.Add(1)
	go func(ctx context.Context, correlationId string, jobResChan chan ServerResponse, clientResChan chan ServerResponse) {
		defer client.pending.Done()
		select {
		case <-ctx.Done():
			// ctx was cancelled or timeout exceeded.
			clientResChan <- ServerResponse{
				nil,
				ctx.Err(),
			}

			// Send cancellation request to worker
			client.jobCancelChan <- correlationId
		case res := <-jobResChan:
			clientResChan <- res
		}

		close(clientResChan)
	}(ctx, msg.CorrelationId, reqJob.resChan, clientResChan)

	return clientResChan, msg.CorrelationId
}

// Shutdown the client and abort any pending requests with ErrCancelled.
// Invoking any client method after invoking Close() will result in an ErrClientClosed
func (client *Client) Close() {
	// Already closed
	if client.ctx.Err() != nil {
		return
	}
	client.cancel()

	// Wait for go-routines to die
	client.pending.Wait()
}
