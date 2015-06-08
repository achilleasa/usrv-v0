package usrv

import "log"

// Server functional option type
type ServerOption func(srv *Server) error

// Use a particular logger instance for logging server events
func WithLogger(logger *log.Logger) ServerOption {
	return func(srv *Server) error {
		srv.Logger = logger

		return nil
	}
}
