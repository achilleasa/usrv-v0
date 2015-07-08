package usrvtest

import (
	"testing"

	"time"

	"bytes"

	"github.com/achilleasa/usrv"
)

func TestInMemoryTransport(t *testing.T) {
	ep := "com.test.server"
	var err error
	var transport usrv.Transport
	transport = NewTransport()

	err = transport.Dial()
	if err != nil {
		t.Fatalf("Error when invoking Dial(): %v", err)
	}

	// Dial while connected should have no effect
	err = transport.Dial()
	if err != nil {
		t.Fatalf("Expected Dial() after connecting not to return an error; got %v", err)
	}

	// Check for error when attempting to send to unknown endpoint
	err = transport.Send(&usrv.Message{
		To: "/dev/null",
	})
	if err == nil || err.Error() != "Unknown destination endpoint" {
		t.Fatalf("Expected Send() to unknown endpoint to fail; got %v", err)
	}

	// Try a client -> server -> client conversation
	serverBinding, err := transport.Bind(usrv.ServerBinding, ep)
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}
	if serverBinding.Name != ep {
		t.Fatalf("Expected server binding to be named %s; got %s", ep, serverBinding.Name)
	}

	clientBinding, err := transport.Bind(usrv.ClientBinding, ep)
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}
	if clientBinding.Name == ep {
		t.Fatalf("Expected client binding NOT to be named %s; got %s", ep, clientBinding.Name)
	}

	msg := &usrv.Message{
		From:    clientBinding.Name,
		To:      serverBinding.Name,
		ReplyTo: clientBinding.Name,
		Payload: []byte("test payload"),
	}

	err = transport.Send(msg)
	if err != nil {
		t.Fatalf("Error while invoking Send(): %s", err)
	}

	var clientReq usrv.TransportMessage
	select {
	case clientReq = <-serverBinding.Messages:
	case <-time.After(time.Second * 1):
		t.Fatalf("Server binding did not receive message after 1 sec")
	}

	if clientReq.Message.From != msg.From {
		t.Fatalf("Received message value mismatch for field 'From'; expected %s got %s", msg.From, clientReq.Message.From)
	}
	if clientReq.Message.To != msg.To {
		t.Fatalf("Received message value mismatch for field 'To'; expected %s got %s", msg.To, clientReq.Message.To)
	}
	if clientReq.Message.ReplyTo != msg.ReplyTo {
		t.Fatalf("Received message value mismatch for field 'ReplyTo'; expected %s got %s", msg.ReplyTo, clientReq.Message.ReplyTo)
	}
	if bytes.Compare(msg.Payload, clientReq.Message.Payload) != 0 {
		t.Fatalf("Received message value mismatch for field 'Message'; expected %v got %v", msg.Payload, clientReq.Message.Payload)
	}

	// Emit response via the response writer
	clientReq.ResponseWriter.Write([]byte("test reply"))
	clientReq.ResponseWriter.Close()

	var serverRes usrv.TransportMessage
	select {
	case serverRes = <-clientBinding.Messages:
	case <-time.After(time.Second * 1):
		t.Fatalf("Client binding did not receive reply message after 1 sec")
	}

	// Expect From/To fields to be reversed
	if serverRes.Message.From != msg.To {
		t.Fatalf("Received message value mismatch for field 'From'; expected %s got %s", msg.To, serverRes.Message.From)
	}
	if serverRes.Message.To != msg.From {
		t.Fatalf("Received message value mismatch for field 'To'; expected %s got %s", msg.From, serverRes.Message.To)
	}
	if serverRes.Message.ReplyTo != "" {
		t.Fatalf("Received message value mismatch for field 'ReplyTo'; expected '' got %s", serverRes.Message.ReplyTo)
	}
	if bytes.Compare([]byte("test reply"), serverRes.Message.Payload) != 0 {
		t.Fatalf("Received message value mismatch for field 'Message'; expected %v got %v", []byte("test reply"), serverRes.Message.Payload)
	}

}

func TestFailMasks(t *testing.T) {
	var err error
	transport := NewTransport()
	transport.SetFailMask(FailDial | FailBind | FailSend)

	err = transport.Dial()
	if err == nil {
		t.Fatalf("Expected Dial() to fail")
	}

	_, err = transport.Bind(usrv.ServerBinding, "foo")
	if err == nil {
		t.Fatalf("Expected Bind() to fail")
	}

	err = transport.Send(nil)
	if err == nil {
		t.Fatalf("Expected Send() to fail")
	}
}

func TestNotifications(t *testing.T) {
	var err error
	transport := NewTransport()

	listener := make(chan error)
	transport.NotifyClose(listener)

	err = transport.Dial()
	if err != nil {
		t.Fatalf("Dial() failed: %v", err)
	}

	_, err = transport.Bind(usrv.ServerBinding, "foo")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	go func() {
		transport.Close()
	}()

	evt, ok := <-listener
	if !ok {
		t.Fatalf("Expected a close error to be sent to the listener channel")
	}
	if evt != usrv.ErrClosed {
		t.Fatalf("Expected listener to receive ErrClosed; got %v", evt)
	}

	// Simulate a reset
	listener = make(chan error)
	transport.NotifyClose(listener)

	err = transport.Dial()
	if err != nil {
		t.Fatalf("Dial() failed: %v", err)
	}

	_, err = transport.Bind(usrv.ServerBinding, "foo")
	if err != nil {
		t.Fatalf("Error binding server endpoint: %v", err)
	}

	go func() {
		transport.Reset()
	}()

	_, ok = <-listener
	if ok {
		t.Fatalf("Expected a listener channel to be closed")
	}
}
