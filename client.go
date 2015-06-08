package usrv

import (
	"code.google.com/p/go-uuid/uuid"
	"errors"
	"golang.org/x/net/context"
	"sync"
)

type ServerResponse struct {
	Msg   *Message
	Error error
}

type job struct {
	msg     *Message
	resChan chan ServerResponse
}

type Client struct {

	// The client root context
	ctx context.Context

	// A context-provided method for shutting down pending requests
	cancel context.CancelFunc

	// The transport used for handling requests
	transport Transport

	// The client transport binding
	binding *Binding

	// This waitgroup tracks ongoing requests so we can
	// properly drain them before terminating the client
	pending sync.WaitGroup

	// pendingMap maps pending Correlation Ids to the channel
	// used for the received response
	pendingMap map[string]chan ServerResponse

	// A channel for submitting requests to the background worker
	workQueue chan job

	endpoint string
}

// Create a new client with default settings
func NewClient(transport Transport, endpoint string) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create server with default settings
	client := &Client{
		ctx:        ctx,
		cancel:     cancel,
		transport:  transport,
		pendingMap: make(map[string]chan ServerResponse),
		workQueue:  make(chan job),
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

func (client *Client) worker() {
	for {
		select {
		case <-client.ctx.Done():
			break
		case transportMsg := <-client.binding.Messages:
			// Try to match the corellation id to a pending request.
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

		case req := <-client.workQueue:

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
		}
	}

	// Fail any pending requests with a timeout error
	for _, resChan := range client.pendingMap {
		resChan <- ServerResponse{
			nil,
			ErrTimeout,
		}
		close(resChan)
	}
}

func (client *Client) Request(ctx context.Context, msg *Message) <-chan ServerResponse {

	// Check the supplied context for the presence of curEndpoint. If it exists
	// we will set it as the "From" field of the outgoing message
	curEndpoint := ctx.Value(CtxCurEndpoint)
	if curEndpoint != nil {
		msg.From = curEndpoint.(string)
	}

	// Assign the private queue name as the message reply target and
	// the target endpoint as the "To" field of the outgoing message.
	// Finally allocate a UUID for matching the async server reply
	// to this request.
	msg.ReplyTo = client.binding.Name
	msg.To = client.endpoint
	msg.CorrelationId = uuid.New()

	// The request uses a buffered channel so our background worker does not
	// block if the client consumer does not read from the returned channel
	reqJob := job{
		msg,
		make(chan ServerResponse, 1),
	}

	// Submit to worker
	client.workQueue <- reqJob

	return reqJob.resChan
}
