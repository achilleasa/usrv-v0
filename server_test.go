package usrv_test

import (
	"testing"

	"time"

	"bytes"

	"errors"

	"io/ioutil"
	"log"

	"fmt"

	"github.com/achilleasa/usrv"
	"github.com/achilleasa/usrv/usrvtest"
	"golang.org/x/net/context"
)

func testHandler(ctx context.Context, rw usrv.ResponseWriter, msg *usrv.Message) {
	rw.Header().Set("foo", "bar")
	rw.Write([]byte("done"))
}

func serverOptionWithError() usrv.ServerOption {
	return func(s *usrv.Server) error {
		return errors.New("Whops")
	}
}

func serverOptionWithoutError() usrv.ServerOption {
	return func(s *usrv.Server) error {
		return nil
	}
}

func middlewareWithError() usrv.EndpointOption {
	return func(ep *usrv.Endpoint) error {
		return errors.New("Middleware error")
	}
}

func TestServer(t *testing.T) {
	var err error

	eventChan := make(chan usrv.ServerEvent, 10)

	transport := usrvtest.NewTransport()

	server, err := usrv.NewServer(
		transport,
		usrv.EventListener(eventChan),
		serverOptionWithoutError(),
		usrv.WithLogger(log.New(ioutil.Discard, "/dev/null", log.LstdFlags)),
	)
	if err != nil {
		t.Fatalf("Error creating server: %v", err)
	}

	err = server.Handle("com.test.foo", usrv.HandlerFunc(testHandler))
	if err != nil {
		t.Fatalf("Error registering handler: %v", err)
	}

	// Run server in background
	go func() {
		err := server.ListenAndServe()
		if err != nil {
			t.Fatalf("Error while listening: %v", err)
		}
	}()

	expectedEvents := []usrv.ServerEvent{
		{usrv.EvtRegistered, "com.test.foo"},
		{usrv.EvtStarted, ""},
		{usrv.EvtServing, "com.test.foo"},
	}

	// Process events till the server has started
	timeout := time.After(time.Second * 5)
pollLoop:
	for {
		select {
		case <-timeout:
			t.Fatalf("Timed-out waiting for server events")
		case event := <-eventChan:
			if event.Type != expectedEvents[0].Type && event.Endpoint != expectedEvents[0].Endpoint {
				t.Fatalf("Expected event %v; got %v", expectedEvents[0], event)
			}

			//
			expectedEvents = expectedEvents[1:]
			if len(expectedEvents) == 0 {
				break pollLoop
			}
		}
	}

	defer server.Close()

	// Create a client for the endpoint
	client := usrv.NewClient(transport)
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()

	reqMsg := &usrv.Message{
		Payload: []byte("test request"),
	}

	resChan, _ := client.Request(context.Background(), reqMsg, "com.test.foo")
	serverRes := <-resChan
	if serverRes.Error != nil {
		t.Fatalf("Server responded with error: %v", serverRes.Error)
	}
	if bytes.Compare(serverRes.Message.Payload, []byte("done")) != 0 {
		t.Fatalf("Wrong server response; expected %v; got %v", []byte("done"), serverRes.Message.Payload)
	}
	if serverRes.Message.Headers.Get("foo") != "bar" {
		t.Fatalf("Wrong response headers for key 'foo'; expected 'bar'; got %v", serverRes.Message.Headers.Get("foo"))
	}
}

func TestServerErrors(t *testing.T) {

	var err error
	var server *usrv.Server

	transport := usrvtest.NewTransport()

	server, err = usrv.NewServer(transport, serverOptionWithError())
	if err == nil {
		t.Fatalf("Expected server creation to fail with: 'Whops'")
	}

	server, err = usrv.NewServer(
		transport,
	)
	if err != nil {
		t.Fatalf("Error creating server: %v", err)
	}

	transport.SetFailMask(usrvtest.FailDial)
	err = server.ListenAndServe()
	if err == nil || err != usrv.ErrDialFailed {
		t.Fatalf("Expected '%s' error; got %v", usrv.ErrDialFailed.Error(), err)
	}

	err = server.Handle("com.test.foo", usrv.HandlerFunc(testHandler), middlewareWithError())
	if err == nil || err.Error() != "Middleware error" {
		t.Fatalf("Expected Handle() to fail with: 'Middleware error'")
	}

	transport.SetFailMask(usrvtest.FailBind)
	server.Handle("com.test.foo", usrv.HandlerFunc(testHandler))
	err = server.ListenAndServe()
	if err == nil || err.Error() != "Bind failed" {
		t.Fatalf("Expected 'Bind failed' error; got %v", err)
	}
}

func TestNonBlockingServerEvents(t *testing.T) {

	var err error
	var server *usrv.Server

	// Use a buffer of 1 to catch at least one message
	eventChan := make(chan usrv.ServerEvent, 1)

	transport := usrvtest.NewTransport()
	server, err = usrv.NewServer(transport, usrv.EventListener(eventChan))
	if err != nil {
		t.Fatalf("Error creating server: %v", err)
	}
	defer server.Close()

	err = server.Handle("com.test.foo", usrv.HandlerFunc(testHandler))
	if err != nil {
		t.Fatalf("Error registering handler: %v", err)
	}
	err = server.Handle("com.test.bar", usrv.HandlerFunc(testHandler))
	if err != nil {
		t.Fatalf("Error registering handler: %v", err)
	}

	expectedEvents := []usrv.ServerEvent{
		{usrv.EvtRegistered, "com.test.foo"},
	}

	// Process events till the server has started
	timeout := time.After(time.Second * 5)
pollLoop:
	for {
		select {
		case <-timeout:
			t.Fatalf("Timed-out waiting for server events")
		case event := <-eventChan:
			if event.Type != expectedEvents[0].Type && event.Endpoint != expectedEvents[0].Endpoint {
				t.Fatalf("Expected event %v; got %v", expectedEvents[0], event)
			}

			expectedEvents = expectedEvents[1:]
			if len(expectedEvents) == 0 {
				break pollLoop
			}
		}
	}

	// The second registration event should be dropped
	select {
	case event := <-eventChan:
		t.Fatalf("Expected second event to be dropped; got %v", event)
	default:
		// This is the expected behavior
	}
}

func TestServerTransportReset(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	eventChan := make(chan usrv.ServerEvent, 10)

	server, err := usrv.NewServer(
		transport,
		usrv.EventListener(eventChan),
		serverOptionWithoutError(),
	)
	if err != nil {
		t.Fatalf("Error creating server: %v", err)
	}

	err = server.Handle("com.test.foo", usrv.HandlerFunc(testHandler))
	if err != nil {
		t.Fatalf("Error registering handler: %v", err)
	}

	// Run server in background
	go func() {
		err := server.ListenAndServe()
		if err != nil {
			t.Fatalf("Error while listening: %v", err)
		}
	}()

	expectedEvents := []usrv.ServerEvent{
		{usrv.EvtRegistered, "com.test.foo"},
		{usrv.EvtStarted, ""},
		{usrv.EvtServing, "com.test.foo"},
	}

	// Process events till the server has started
	timeout := time.After(time.Second * 5)
pollLoop:
	for {
		select {
		case <-timeout:
			t.Fatalf("Timed-out waiting for server events")
		case event := <-eventChan:
			if event.Type != expectedEvents[0].Type && event.Endpoint != expectedEvents[0].Endpoint {
				t.Fatalf("Expected event %v; got %v", expectedEvents[0], event)
			}

			//
			expectedEvents = expectedEvents[1:]
			if len(expectedEvents) == 0 {
				break pollLoop
			}
		}
	}

	defer server.Close()

	// Reset
	transport.Reset()

	// Wait for server to rebind
	expectedEvents = []usrv.ServerEvent{
		{usrv.EvtTransportReset, ""},
		{usrv.EvtStarted, ""},
		{usrv.EvtServing, "com.test.foo"},
	}
	timeout = time.After(time.Second * 5)
pollLoop1:
	for {
		select {
		case <-timeout:
			t.Fatalf("Timed-out waiting for server events")
		case event := <-eventChan:
			if event.Type != expectedEvents[0].Type && event.Endpoint != expectedEvents[0].Endpoint {
				t.Fatalf("Expected event %v; got %v", expectedEvents[0], event)
			}

			expectedEvents = expectedEvents[1:]
			if len(expectedEvents) == 0 {
				break pollLoop1
			}
		}
	}

	// Create a client for the endpoint
	client := usrv.NewClient(transport)
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()
	//
	reqMsg := &usrv.Message{
		Payload: []byte("test request"),
	}
	resChan, _ := client.Request(context.Background(), reqMsg, "com.test.foo")
	serverRes := <-resChan
	if serverRes.Error != nil {
		t.Fatalf("Server responded with error: %v", serverRes.Error)
	}
	if bytes.Compare(serverRes.Message.Payload, []byte("done")) != 0 {
		t.Fatalf("Wrong server response; expected %v; got %v", []byte("done"), serverRes.Message.Payload)
	}
	client.Close()
}

func TestServerNonRecoverableTransportReset(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	eventChan := make(chan usrv.ServerEvent, 10)

	server, err := usrv.NewServer(
		transport,
		usrv.EventListener(eventChan),
		serverOptionWithoutError(),
	)
	if err != nil {
		t.Fatalf("Error creating server: %v", err)
	}

	err = server.Handle("com.test.foo", usrv.HandlerFunc(testHandler))
	if err != nil {
		t.Fatalf("Error registering handler: %v", err)
	}

	// Run server in background
	done := make(chan struct{})
	go func() {
		defer func() {
			// Recover panic error
			err := recover()
			if err == nil || err != usrv.ErrDialFailed.Error() {
				t.Fail()
				panic(fmt.Sprintf("Expected server to panic with '%s'; got %v", usrv.ErrDialFailed.Error(), err))
			}

			done <- struct{}{}
		}()

		err := server.ListenAndServe()
		if err != nil {
			t.Fatalf("Error while listening: %v", err)
		}
	}()

	expectedEvents := []usrv.ServerEvent{
		{usrv.EvtRegistered, "com.test.foo"},
		{usrv.EvtStarted, ""},
		{usrv.EvtServing, "com.test.foo"},
	}

	// Process events till the server has started
	timeout := time.After(time.Second * 5)
pollLoop:
	for {
		select {
		case <-timeout:
			t.Fatalf("Timed-out waiting for server events")
		case event := <-eventChan:
			if event.Type != expectedEvents[0].Type && event.Endpoint != expectedEvents[0].Endpoint {
				t.Fatalf("Expected event %v; got %v", expectedEvents[0], event)
			}

			//
			expectedEvents = expectedEvents[1:]
			if len(expectedEvents) == 0 {
				break pollLoop
			}
		}
	}

	defer server.Close()

	// Reset
	transport.SetFailMask(usrvtest.FailDial)
	transport.Reset()

	// Wait for server to panic
	<-done
}

func TestServerTransportShutdown(t *testing.T) {

	var err error

	transport := usrvtest.NewTransport()

	eventChan := make(chan usrv.ServerEvent, 10)

	server, err := usrv.NewServer(
		transport,
		usrv.EventListener(eventChan),
	)
	if err != nil {
		t.Fatalf("Error creating server: %v", err)
	}

	go func() {
		server.ListenAndServe()
	}()

	// Wait for server to start
	<-eventChan

	// Close transport
	transport.Close()

	server.Close()
}
