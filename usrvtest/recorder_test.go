package usrvtest

import (
	"errors"
	"testing"

	"bytes"

	"github.com/achilleasa/usrv"
)

func TestRecorder(t *testing.T) {
	var rec *ResponseRecorder
	var header interface{}
	var err error

	// Test write
	data := []byte("test")
	rec = NewRecorder()
	count, err := rec.Write(data)
	if err != nil {
		t.Fatalf("Write() failed: %s", err)
	}
	if count != len(data) {
		t.Fatalf("Wrote %d bytes; expected %d", count, len(data))
	}
	if bytes.Compare(rec.Message.Payload, data) != 0 {
		t.Fatalf("Expected 'test' payload to be written; got: %v", rec.Message.Payload)
	}

	// Test error
	testError := errors.New("test error")
	rec = NewRecorder()
	err = rec.WriteError(testError)
	if err != nil {
		t.Fatalf("WriteError() failed: %s", err)
	}
	header = rec.Header().Get("error")
	if header == nil || header.(string) != "test error" {
		t.Fatalf("Expected 'test error' to be written; got: %v", header)
	}

	// Test headers
	rec = NewRecorder()
	rec.Header().Set("foo", "bar")
	header = rec.Header().Get("foo")
	if header == nil || header.(string) != "bar" {
		t.Fatalf("Expected 'foo' header with value 'bar' to be written; got: %v", header)
	}

	// Test flush
	rec = NewRecorder()
	err = rec.Close()
	if err != nil {
		t.Fatalf("Close() failed with %v", err)
	}

	count, err = rec.Write(data)
	if err == nil || err != usrv.ErrResponseSent {
		t.Fatalf("Write() after flush should fail with ErrResponseSent; got: %v", err)
	}

	err = rec.WriteError(testError)
	if err == nil || err != usrv.ErrResponseSent {
		t.Fatalf("WriteError() after flush should fail with ErrResponseSent; got: %v", err)
	}

	err = rec.Close()
	if err == nil || err != usrv.ErrResponseSent {
		t.Fatalf("Close() after flush should fail with ErrResponseSent; got: %v", err)
	}

}
