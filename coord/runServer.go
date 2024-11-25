package coord

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// RunWithSignals runs the http server until it shutsdown or
// an interrupt signal is received. On interrupt, a graceful
// shutdown is attempted for graceTime before performing a hard shutdown.
func RunWithSignals(s *http.Server, graceTime time.Duration) error {
	done := make(chan error, 1)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	go func() { done <- s.ListenAndServe() }()

	var err error
	select {
	case <-sig:
		ctx, _ := context.WithTimeout(context.Background(), graceTime)
		s.Shutdown(ctx)
		s.Close()
		err = <-done
	case err = <-done:
		// continue...
	}

	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
