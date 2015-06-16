package usrv_test

import (
	"testing"

	"bytes"

	"time"

	"github.com/achilleasa/usrv"
	"github.com/achilleasa/usrv/usrvtest"
	"golang.org/x/net/context"
)

func TestClient(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	_, err = transport.Bind(context.Background(), usrv.ServerBinding, "com.test.server")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	client, err := usrv.NewClient(transport, "com.test.server")
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()

	reqMsg := &usrv.Message{
		Payload: []byte("test request"),
	}
	resChan := client.Request(context.Background(), reqMsg)

	// Now that the reqMsg has been populated by the client use the data to
	// setup a response
	resMsg := &usrv.Message{
		From:          "com.test.server",
		To:            reqMsg.ReplyTo,       // reply to the private endpoint specified by the client
		CorrelationId: reqMsg.CorrelationId, // use generated correlationId
		Payload:       []byte("test response"),
	}

	// Enqueue response and wait for the client to pick it up
	transport.Send(resMsg)

	serverRes := <-resChan
	if serverRes.Error != nil {
		t.Fatalf("Client request failed with error: %v", serverRes.Error)
	}

	if bytes.Compare(serverRes.Message.Payload, resMsg.Payload) != 0 {
		t.Fatalf("Client response mismatch; expected %v, got %v", resMsg.Payload, serverRes.Message)
	}
}

func TestRequestTimeout(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	_, err = transport.Bind(context.Background(), usrv.ServerBinding, "com.test.server")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	client, err := usrv.NewClient(transport, "com.test.server")
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()

	reqMsg := &usrv.Message{
		Payload: []byte("test request"),
	}
	resChan := client.RequestWithTimeout(context.Background(), reqMsg, time.Millisecond*1)

	// Wait for timeout
	select {
	case serverRes := <-resChan:
		if serverRes.Error != usrv.ErrTimeout {
			t.Fatalf("Request did not fail with ErrTimeout; got %v", serverRes.Error)
		}
	case <-time.After(time.Second):
		t.Fatalf("Request did not timeout after 1 second")
	}

}

func TestServerError(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	serverBinding, err := transport.Bind(context.Background(), usrv.ServerBinding, "com.test.server")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	client, err := usrv.NewClient(transport, "com.test.server")
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()

	type testSpec struct {
		errMsg string
		err    error
	}

	testProvider := []testSpec{
		{
			usrv.ErrTimeout.Error(),
			usrv.ErrTimeout,
		},
		{
			usrv.ErrCancelled.Error(),
			usrv.ErrCancelled,
		},
		{
			"Random error",
			nil,
		},
	}

	for _, test := range testProvider {
		reqMsg := &usrv.Message{
			Payload: []byte("test request"),
		}
		resChan := client.Request(context.Background(), reqMsg)

		// Now that the reqMsg has been populated by the client use the data to
		// setup a response with an error
		resMsg := &usrv.Message{
			From:          "com.test.server",
			To:            reqMsg.ReplyTo,       // reply to the private endpoint specified by the client
			CorrelationId: reqMsg.CorrelationId, // use generated correlationId
			Headers:       usrv.Header{"error": test.errMsg},
		}

		// Dequeue request, enqueue response and wait for the client to pick it up
		<-serverBinding.Messages
		transport.Send(resMsg)

		serverRes := <-resChan
		if serverRes.Error == nil {
			t.Fatalf("Expected client request to fail")
		}

		if serverRes.Error.Error() != test.errMsg {
			t.Fatalf("Expected client request to fail with a server-side message; expected %v, got %v", test.errMsg, serverRes.Error.Error())
		}
		if test.err != nil && serverRes.Error != test.err {
			t.Fatalf("Incorrect client error mapping; expected %v, got %v", test.err, serverRes.Error)
		}
	}

}

func TestClientClose(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	_, err = transport.Bind(context.Background(), usrv.ServerBinding, "com.test.server")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	client, err := usrv.NewClient(transport, "com.test.server")
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	reqMsg := &usrv.Message{
		Payload: []byte("test request"),
	}
	resChan := client.Request(context.Background(), reqMsg)

	// Close client and wait for the response
	client.Close()
	serverRes := <-resChan
	if serverRes.Error == nil {
		t.Fatalf("Expected client request to fail")
	}

	if serverRes.Error != usrv.ErrClosed {
		t.Fatalf("Expected request to fail with ErrClosed; got %v", serverRes.Error)
	}

	// Requests on a closed client should automatically fail
	resChan = client.Request(context.Background(), reqMsg)
	serverRes = <-resChan
	if serverRes.Error != usrv.ErrClosed {
		t.Fatalf("Expected Request() when client is closed to fail with ErrClosed; got %v", serverRes.Error)
	}
	resChan = client.RequestWithTimeout(context.Background(), reqMsg, time.Millisecond)
	serverRes = <-resChan
	if serverRes.Error != usrv.ErrClosed {
		t.Fatalf("Expected RequestWithTimeout() when client is closed to fail with ErrClosed; got %v", serverRes.Error)
	}

	// Close should be a nop now
	client.Close()
}

func TestHandleReplyWithUnknownId(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	_, err = transport.Bind(context.Background(), usrv.ServerBinding, "com.test.server")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	client, err := usrv.NewClient(transport, "com.test.server")
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()

	reqMsg := &usrv.Message{
		Payload: []byte("test request"),
	}
	resChan := client.Request(context.Background(), reqMsg)

	// Send a mock response to the client with an unknown correlation id
	resMsg := &usrv.Message{
		From:          "com.test.server",
		To:            reqMsg.ReplyTo, // reply to the private endpoint specified by the client
		CorrelationId: "0xdeadbeed",
		Payload:       []byte("I see unknown requests"),
	}
	transport.Send(resMsg)

	// Send the correct response
	resMsg = &usrv.Message{
		From:          "com.test.server",
		To:            reqMsg.ReplyTo,       // reply to the private endpoint specified by the client
		CorrelationId: reqMsg.CorrelationId, // use generated correlationId
		Payload:       []byte("test response"),
	}
	transport.Send(resMsg)

	serverRes := <-resChan
	if serverRes.Message.CorrelationId != reqMsg.CorrelationId {
		t.Fatalf("Received wrong server reply; expected correlation id %s; got %s", reqMsg.CorrelationId, serverRes.Message.CorrelationId)
	}
}

func TestClientHeaders(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	serverBinding, err := transport.Bind(context.Background(), usrv.ServerBinding, "com.test.server")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	client, err := usrv.NewClient(transport, "com.test.server")
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()

	type header struct {
		header string
		value  interface{}
	}

	headerSpec := []header{
		{
			usrv.CtxCurEndpoint,
			"com.test.client",
		},
		{
			usrv.CtxTraceId,
			"trace-1",
		},
	}

	ctx := context.Background()
	for _, h := range headerSpec {
		ctx = context.WithValue(ctx, h.header, h.value)
	}

	reqMsg := &usrv.Message{
		Payload: []byte("test request"),
	}
	client.Request(ctx, reqMsg)

	clientReq := <-serverBinding.Messages

	// Check that the correct headers are filled in
	v := clientReq.Message.Headers.Get(usrv.CtxTraceId)
	if v != "trace-1" {
		t.Fatalf("Expected header %s = %s to be set; got value %s", usrv.CtxTraceId, "trace-1", v)
	}

	// Check From field
	if clientReq.Message.From != "com.test.client" {
		t.Fatalf("Expected outgoing request From field value to be %s; got %s", "com.test.client", clientReq.Message.From)
	}
}

func TestTransportErrors(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()
	transport.SetFailMask(usrvtest.FailDial)

	_, err = transport.Bind(context.Background(), usrv.ServerBinding, "com.test.server")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	client, err := usrv.NewClient(transport, "com.test.server")
	if err == nil || err.Error() != "Dial failed" {
		t.Fatalf("Expected 'Dial failed' error; got %v", err)
	}

	transport.SetFailMask(usrvtest.FailBind)
	client, err = usrv.NewClient(transport, "com.test.server")
	if err == nil || err.Error() != "Bind failed" {
		t.Fatalf("Expected 'Bind failed' error; got %v", err)
	}

	transport.SetFailMask(usrvtest.FailSend)
	client, err = usrv.NewClient(transport, "com.test.server")
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}

	reqMsg := &usrv.Message{
		Payload: []byte("test request"),
	}
	resChan := client.Request(context.Background(), reqMsg)

	serverRes := <-resChan
	if serverRes.Error == nil || serverRes.Error.Error() != "Send failed" {
		t.Fatalf("Expected 'Send failed' error; got %v", err)
	}
}