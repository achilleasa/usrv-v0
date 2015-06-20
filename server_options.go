package usrv

import "log"

// Server functional option type.
type ServerOption func(srv *Server) error

// Use a particular logger instance for logging server events.
func WithLogger(logger *log.Logger) ServerOption {
	return func(srv *Server) error {
		srv.Logger = logger

		return nil
	}
}

// Attach an event listener for server-generated events
func EventListener(listener chan ServerEvent) ServerOption {
	return func(srv *Server) error {
		srv.eventListeners = append(srv.eventListeners, listener)

		return nil
	}
}
