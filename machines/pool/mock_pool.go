package pool

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"
)

const mockMachId = "m8001"
const mockInstanceId = "INSTANCEID"
const mockUrl = "http://localhost:8001"

// MockPool is a mock pool of machines of size 1.
type MockPool struct {
	cmd string
	arg []string

	mach   *Mach
	free   chan *Mach
	cancel context.CancelFunc
}

var _ Pool = (*MockPool)(nil)

// NewMock creates a mock pool.
func NewMock(cmd string, arg ...string) *MockPool {
	p := &MockPool{
		cmd:  cmd,
		arg:  arg,
		free: make(chan *Mach, 1),
	}

	// Add one mach to the pool.
	mach := &Mach{
		Url:        mockUrl,
		Id:         mockMachId,
		InstanceId: mockInstanceId,
	}
	mach.Free = func() { p.freeMach(mach) }
	p.mach = mach
	p.free <- mach

	return p
}

func (p *MockPool) Close() error {
	log.Printf("mock pool: close")
	p.mach.Free()
	close(p.free)
	return nil
}

func (p *MockPool) Destroy() error {
	return p.Close()
}

func (p *MockPool) Alloc(ctx context.Context) (*Mach, error) {
	log.Printf("mock pool: alloc wait")
	var mach *Mach
	select {
	case <-ctx.Done():
		log.Printf("mock pool: alloc cancelled context")
		return nil, ctx.Err()
	case mach = <-p.free:
		if mach == nil {
			log.Printf("mock pool: alloc cancelled with closed pool")
			return nil, ErrPoolClosed
		}
		// continue with mach...
	}

	log.Printf("mock pool: starting machine %s", mach.Id)
	cmdctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdctx, p.cmd, p.arg...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Command.Start: %w", err)
	}

	// hack: give the process a little time to start up.
	time.Sleep(time.Second)
	p.cancel = cancel

	log.Printf("mock pool: started machine %s", mach.Id)
	return mach, nil
}

func (p *MockPool) freeMach(mach *Mach) {
	log.Printf("pool: free %s", mach.Id)
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
		p.free <- mach
	}
}
