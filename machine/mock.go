package machine

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"
)

// MockApi provides an API that just runs the process on
// the same machine instead of spinning up a new machine.
type MockApi struct {
	cmd string
	arg []string
}

var _ Api = (*MockApi)(nil)

// NewMock "starts" a machine by running cmd and stops it by
// killing the process.
func NewMock(cmd string, arg ...string) Api {
	return &MockApi{cmd, arg}
}

func (p *MockApi) Start() (Machine, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, p.cmd, p.arg...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Command.Start: %w", err)
	}

	log.Printf("starting machine %s", mockMachId)

	// hack: give the process a little time to start up.
	time.Sleep(time.Second)

	log.Printf("start machine %s", mockMachId)
	return &MockMachine{cmd, cancel}, nil
}

const mockMachId = "m8001"
const mockAddr = "localhost:8001"

type MockMachine struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

var _ Machine = (*MockMachine)(nil)

func (p *MockMachine) Info() MachineInfo {
	return MachineInfo{
		Id:   mockMachId,
		Addr: mockAddr,
	}
}

func (p *MockMachine) Stop() error {
	log.Printf("stop machine %s", mockMachId)
	p.cancel()
	p.cmd.Wait()
	log.Printf("stopped machine %s", mockMachId)
	return nil
}
