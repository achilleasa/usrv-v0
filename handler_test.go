package usrv_test

import (
	"testing"

	"encoding/json"

	"errors"

	"github.com/achilleasa/usrv"
	"github.com/achilleasa/usrv/usrvtest"
	"golang.org/x/net/context"
)

func TestPipelineHandler(t *testing.T) {
	decoder := func(data []byte) (interface{}, error) {
		var payload interface{}
		err := json.Unmarshal(data, &payload)
		return payload, err
	}
	processor := func(ctx context.Context, decodedData interface{}) (interface{}, error) {

		data := decodedData.(map[string]interface{})

		if _, exists := data["foo"]; !exists {
			t.Fatalf("Expected unmarshalled data to contain a key called 'foo'")
		}

		data["baz"] = "baz"
		return data, nil
	}
	encoder := json.Marshal

	ep := usrv.Endpoint{
		Name:    "pipelineTest",
		Handler: usrv.PipelineHandler{decoder, processor, encoder},
	}

	var jsonBlob = []byte(`{"foo" : "bar"}`)
	rw := usrvtest.NewRecorder()
	ep.Handler.Serve(context.Background(), rw, &usrv.Message{Payload: jsonBlob})

	// Unpack response
	var out map[string]interface{}
	json.Unmarshal(rw.Message.Payload, &out)

	if out["baz"] != "baz" {
		t.Fatalf("Expected response payload to contain a key 'baz' with value 'baz'; got %v", out)
	}
}

func TestDecoderError(t *testing.T) {
	decoder := func(data []byte) (interface{}, error) {
		return nil, errors.New("decoder error")
	}
	processor := func(ctx context.Context, decodedData interface{}) (interface{}, error) {
		return nil, nil
	}
	encoder := json.Marshal

	ep := usrv.Endpoint{
		Name:    "pipelineTest",
		Handler: usrv.PipelineHandler{decoder, processor, encoder},
	}

	var jsonBlob = []byte(`{"foo" : "bar"}`)
	rw := usrvtest.NewRecorder()
	ep.Handler.Serve(context.Background(), rw, &usrv.Message{Payload: jsonBlob})

	header := rw.Header().Get("error")
	if header == nil || header.(string) != "decoder error" {
		t.Fatalf("Expected 'decoder error' to be written; got: %v", header)
	}
}

func TestProcessorError(t *testing.T) {
	decoder := func(data []byte) (interface{}, error) {
		return "", nil
	}
	processor := func(ctx context.Context, decodedData interface{}) (interface{}, error) {
		return nil, errors.New("processor error")
	}
	encoder := json.Marshal

	ep := usrv.Endpoint{
		Name:    "pipelineTest",
		Handler: usrv.PipelineHandler{decoder, processor, encoder},
	}

	var jsonBlob = []byte(`{"foo" : "bar"}`)
	rw := usrvtest.NewRecorder()
	ep.Handler.Serve(context.Background(), rw, &usrv.Message{Payload: jsonBlob})

	header := rw.Header().Get("error")
	if header == nil || header.(string) != "processor error" {
		t.Fatalf("Expected 'processor error' to be written; got: %v", header)
	}
}

func TestEncoderError(t *testing.T) {
	decoder := func(data []byte) (interface{}, error) {
		return nil, nil
	}
	processor := func(ctx context.Context, decodedData interface{}) (interface{}, error) {
		return nil, nil
	}
	encoder := func(data interface{}) ([]byte, error) {
		return nil, errors.New("encoder error")
	}

	ep := usrv.Endpoint{
		Name:    "pipelineTest",
		Handler: usrv.PipelineHandler{decoder, processor, encoder},
	}

	var jsonBlob = []byte(`{"foo" : "bar"}`)
	rw := usrvtest.NewRecorder()
	ep.Handler.Serve(context.Background(), rw, &usrv.Message{Payload: jsonBlob})

	header := rw.Header().Get("error")
	if header == nil || header.(string) != "encoder error" {
		t.Fatalf("Expected 'encoder error' to be written; got: %v", header)
	}
}
