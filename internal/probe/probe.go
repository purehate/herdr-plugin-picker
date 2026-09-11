// Package probe checks TCP reachability of SSH hosts. Results stream so the
// picker can render before the network answers.
package probe

import (
	"context"
	"net"
	"sync"
	"time"
)

// maxInFlight bounds concurrent dials. A large ssh config should not open a
// hundred sockets at once just to draw a status dot.
const maxInFlight = 16

// Target is one host to check. Skip marks hosts that cannot be reached
// directly, such as anything behind a ProxyJump or ProxyCommand.
type Target struct {
	Alias string
	Addr  string
	Skip  bool
}

// Result reports one host's reachability.
type Result struct {
	Alias string
	Up    bool
}

// dialFn dials one probe target. It is a named type because three call sites
// read it and the seam should look deliberate rather than incidental.
type dialFn func(ctx context.Context, timeout time.Duration, addr string) (net.Conn, error)

// dialContext is the production dialFn.
func dialContext(ctx context.Context, timeout time.Duration, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp", addr)
}

// Run dials every non-skipped target and streams results. The returned channel
// always closes, including on a canceled context.
//
// This delegation is pinned by TestRunForwardsItsTimeoutToTheDial and
// TestRunForwardsItsContextToTheDial, which go through Run rather than run and
// so cover the hop the argument-observing tests bypass.
//
// Both dial a LIVE loopback listener, and that is load-bearing rather than
// incidental. Result carries only Alias and Up, so against a closed port
// context.Canceled, i/o timeout and connection refused all collapse to
// Up: false and nothing discriminates. A listener that accepts inverts it:
// only a dropped timeout or a dropped context lets the connect succeed and
// report Up: true. Do not "simplify" those addresses to a closed port -- it
// silently un-pins both hops while leaving the tests green.
func Run(ctx context.Context, targets []Target, timeout time.Duration) <-chan Result {
	return run(ctx, targets, timeout, dialContext)
}

// run is Run with the dial injected, so tests can supply a fake without
// opening real sockets.
//
// The dial is a parameter rather than a package-level variable on purpose.
// As a variable it was shared mutable state: every test that wanted a fake
// assigned it and restored it with a defer, so two such tests running under
// t.Parallel() raced both each other and Run's own read of it. Passing it in
// makes that race unrepresentable rather than merely discouraged, which is
// what lets the tests below call t.Parallel().
func run(ctx context.Context, targets []Target, timeout time.Duration, dial dialFn) <-chan Result {
	out := make(chan Result)
	sem := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup

	for _, t := range targets {
		if t.Skip {
			continue
		}
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			conn, err := dial(ctx, timeout, t.Addr)
			if err == nil {
				// The dial succeeding is the whole answer; a close error says
				// nothing about reachability.
				_ = conn.Close()
			}
			select {
			case out <- Result{Alias: t.Alias, Up: err == nil}:
			case <-ctx.Done():
			}
		}(t)
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
