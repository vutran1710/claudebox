package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// DefaultAddr binds loopback only.
//
// Over plain HTTP a listener on 0.0.0.0 puts the bearer key, every prompt and
// every answer on the wire in cleartext, and the key is the whole of the
// authentication. Reaching this from elsewhere is a deliberate act — an ssh
// port-forward, or a tunnel that terminates HTTPS.
const DefaultAddr = "127.0.0.1:8091"

// Options are what `cbx serve` was asked for.
type Options struct {
	Addr string
	Out  io.Writer
}

// Serve runs the server until the process is signalled.
//
// It holds the lock for its lifetime, reconciles the wreckage of any previous
// incarnation, and starts the janitor. Returns when shut down cleanly.
func (s *Server) Serve(ctx context.Context, opts Options) error {
	lock, err := AcquireLock(DefaultLockPath())
	if err != nil {
		return err
	}
	defer lock.Release()

	// Job rows outlive the claude -p children that produce them. Without this
	// a client polling after a restart sees "running" for ever — a ghost, and
	// a lie, since nothing is working on it. It also clears the unique index,
	// so no session stays wedged by a job that died with the last process.
	if n, err := s.Store.RecoverRunningJobs(); err != nil {
		return err
	} else if n > 0 {
		fmt.Fprintf(opts.Out, "interrupted\t%d\n", n)
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go s.Janitor(ctx, SweepInterval)

	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", opts.Addr, err)
	}
	srv := &http.Server{
		Handler: s.Handler(),
		// Must exceed MaxRespondWithin, or the transport closes connections
		// the parameter explicitly permits.
		WriteTimeout: MaxRespondWithin + time.Minute,
		// Short, and unrelated to how long a handler may run: this is what
		// defends against a client that sends headers slowly and never stops.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	fmt.Fprintf(opts.Out, "addr\thttp://%s\n", ln.Addr())
	fmt.Fprintf(opts.Out, "pid\t%d\n", os.Getpid())
	fmt.Fprintf(opts.Out, "key\t%s\n", DefaultKeyPath())
	fmt.Fprintf(opts.Out, "commands\t%s\n", s.SpecPath)

	errc := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// Give a query in flight a moment to land in its job before the process
	// goes; anything still running is recovered on the next start.
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
