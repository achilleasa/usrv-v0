package usrv

type EventType int

const (
	EvtRegistered EventType = iota // Registered endpoint
	EvtStarted              = iota // Dialed transport
	EvtServing              = iota // Serving endpoint
	EvtStopping             = iota // Stopping server
	EvtStopped              = iota // Server stopped and requests drained
)

type ServerEvent struct {
	Type     EventType
	Endpoint string
}
