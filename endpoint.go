package usrv

// Endpoint functional option type.
type EndpointOption func(ep *Endpoint) error

// A service endpoint.
type Endpoint struct {
	// The name of this endpoint. To avoid name clashes between similarly named
	// endpoints in different packages you should supply the fully qualified name
	// of this endpoint (e.g. com.foo.bar.svc1)
	Name string

	// The handler that will service incoming requests to this endpoint.
	Handler Handler
}
