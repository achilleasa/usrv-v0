package usrv

import (
	"errors"

	"golang.org/x/net/context"
)

// Errors introduced by the server and the client.
var (
	ErrCancelled    = context.Canceled
	ErrTimeout      = context.DeadlineExceeded
	ErrResponseSent = errors.New("Response already sent")
	ErrClosed       = errors.New("Connection closed")
	ErrDialFailed   = errors.New("Failed to connect")
)
