package machine

// Api provides machine API services.
type Api interface {
	Start() (Machine, error)
}

// MachineInfo returns information about a started machine.
type MachineInfo struct {
	Id   string
	Addr string
}

// Machine is an interface on a started machine.
type Machine interface {
	Info() MachineInfo
	Stop() error
}
