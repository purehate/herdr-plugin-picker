package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// liveListener returns the address of a loopback listener that accepts for the
// duration of the test. Tests that go through Run need a socket that actually
// answers; see the note on Run about why a closed port cannot substitute.
func liveListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// drain collects every result until the channel closes. It doubles as the
// assertion that the channel closes at all: a Run that leaks would hang here.
func drain(ch <-chan Result) []Result {
	var got []Result
	for r := range ch {
		got = append(got, r)
	}
	return got
}

func TestRunReportsReachability(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	targets := []Target{
		{Alias: "up", Addr: ln.Addr().String()},
		{Alias: "down", Addr: "127.0.0.1:1"},
		{Alias: "skipped", Addr: "127.0.0.1:1", Skip: true},
	}

	results := map[string]bool{}
	for r := range Run(context.Background(), targets, 500*time.Millisecond) {
		results[r.Alias] = r.Up
	}

	if len(results) != 2 {
		t.Fatalf("results = %v, want 2 entries (skipped host omitted)", results)
	}
	if !results["up"] {
		t.Error("listening host reported down")
	}
	if results["down"] {
		t.Error("closed port reported up")
	}
	if _, ok := results["skipped"]; ok {
		t.Error("skipped target was probed")
	}
}

func TestRunClosesChannelOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := Run(ctx, []Target{{Alias: "a", Addr: "127.0.0.1:1"}}, time.Second)
	for range ch {
		// Drain: results may or may not arrive, but the channel must close.
	}
}

func TestRunBoundsConcurrentDials(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var cur, peak int
	release := make(chan struct{})

	dial := func(context.Context, time.Duration, string) (net.Conn, error) {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		mu.Unlock()
		<-release // hold the slot so concurrency can accumulate
		mu.Lock()
		cur--
		mu.Unlock()
		// A nil conn is safe: Run only calls Close when err is nil.
		return nil, errors.New("probe: test dial")
	}

	// Fixed, and deliberately NOT derived from maxInFlight. A test written in
	// terms of the constant it is testing scales with its own mutant.
	const nTargets = 64
	targets := make([]Target, 0, nTargets)
	for i := 0; i < nTargets; i++ {
		targets = append(targets, Target{Alias: fmt.Sprintf("h%02d", i), Addr: "192.0.2.1:22"})
	}

	out := run(context.Background(), targets, time.Second, dial)

	// Wait for the bound to be reached, then give it room to be exceeded. A
	// correct Run pins cur at exactly maxInFlight; an unbounded one runs
	// straight past it to len(targets).
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := cur
		mu.Unlock()
		if got >= maxInFlight || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	close(release)

	n := 0
	for range out {
		n++
	}

	if gotPeak != maxInFlight {
		t.Errorf("peak concurrent dials = %d, want %d", gotPeak, maxInFlight)
	}
	if n != len(targets) {
		t.Errorf("got %d results, want %d", n, len(targets))
	}
}

// TestRunForwardsTimeoutAndAddrToTheDial pins that Run hands the dial each
// target's own Addr and its own timeout argument.
//
// This is the second thing the injected dial seam bought, beyond the
// concurrency bound it was added for. Before the seam, timeout was consumed
// inside net.Dialer and observable only as wall-clock, so replacing it with 0
// survived the entire suite; as an argument to an injected function a fake
// reads it directly, with no timing and no network.
//
// Scope note: run holds exactly one timeout value and passes it to every dial,
// so this pins "each addr was dialed, each with Run's timeout" -- not a
// per-target pairing. No mutation of current code can route a timeout to the
// wrong target, because there is no per-target timeout to route. The map
// keying guards that property if one is ever introduced.
//
// The two ports differ (:22 vs :2222) so a prefix comparison cannot pass.
func TestRunForwardsTimeoutAndAddrToTheDial(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	got := map[string]time.Duration{}

	dial := func(_ context.Context, timeout time.Duration, addr string) (net.Conn, error) {
		mu.Lock()
		got[addr] = timeout
		mu.Unlock()
		return nil, errors.New("probe: test dial")
	}

	targets := []Target{
		{Alias: "a", Addr: "192.0.2.1:22"},
		{Alias: "b", Addr: "192.0.2.2:2222"},
	}
	for range run(context.Background(), targets, 7*time.Second, dial) {
	}

	mu.Lock()
	defer mu.Unlock()
	want := map[string]time.Duration{"192.0.2.1:22": 7 * time.Second, "192.0.2.2:2222": 7 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("dialed %d addrs, want %d: %v", len(got), len(want), got)
	}
	for addr, w := range want {
		if g, ok := got[addr]; !ok {
			t.Errorf("addr %q never dialed", addr)
		} else if g != w {
			t.Errorf("addr %q got timeout %v, want %v", addr, g, w)
		}
	}
}

// ctxKey keys the sentinel that TestRunForwardsContextToTheDial puts in the
// context. It is an unexported struct type so no other package can collide
// with it.
type ctxKey struct{}

// TestRunForwardsContextToTheDial pins that Run hands the dial the caller's
// context rather than a fresh one.
//
// This is the third dial argument the injected dial seam made observable.
// Before the seam, ctx reached the network only inside net.Dialer.DialContext
// and its
// sole observable consequence was cancellation timing, so a mutant
// substituting context.Background() survived the whole suite -- including
// TestRunClosesChannelOnCanceledContext, whose goroutine exits at the
// semaphore select or at the send and never reaches the dial's context.
//
// The assertion is a value lookup, deliberately not a nil check:
// context.Background() is non-nil, so a nil check would pass the exact mutant
// this test exists to kill.
//
// Scope note, same shape as TestRunForwardsTimeoutAndAddrToTheDial: run passes
// one context to every dial, so this pins "each dial received the caller's
// context", not per-target routing. No mutation of current code can route a
// context to the wrong target, because there is no per-target context. The
// per-addr map guards that property if one is ever introduced; it is not a
// live discriminator today.
func TestRunForwardsContextToTheDial(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	got := map[string]any{}

	dial := func(ctx context.Context, _ time.Duration, addr string) (net.Conn, error) {
		mu.Lock()
		got[addr] = ctx.Value(ctxKey{})
		mu.Unlock()
		return nil, errors.New("probe: test dial")
	}

	targets := []Target{
		{Alias: "a", Addr: "192.0.2.1:22"},
		{Alias: "b", Addr: "192.0.2.2:2222"},
	}
	ctx := context.WithValue(context.Background(), ctxKey{}, "sentinel")
	for range run(ctx, targets, time.Second, dial) {
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != len(targets) {
		t.Fatalf("dialed %d addrs, want %d: %v", len(got), len(targets), got)
	}
	for _, tgt := range targets {
		if v, ok := got[tgt.Addr]; !ok {
			t.Errorf("addr %q never dialed", tgt.Addr)
		} else if v != "sentinel" {
			t.Errorf("addr %q got ctx value %v, want %q", tgt.Addr, v, "sentinel")
		}
	}
}

// TestRunForwardsItsTimeoutToTheDial pins Run's delegation to run, which the
// two tests above cannot see: they call run directly and so skip the hop.
//
// A 1ns timeout against a listener that WOULD accept can only report Up if the
// timeout was dropped on the way through. net.Dialer turns Timeout into a
// context deadline that is already expired, and dialSerial's pre-address
// select has a default arm, so the expired-deadline branch is taken without a
// race: not merely usually false, deterministically false.
//
// The assertion is "nothing came back Up", not "one result came back". Under a
// dropped timeout the channel still delivers exactly one result, so counting
// results discriminates nothing; and see the sibling test for why counting is
// affirmatively wrong there.
func TestRunForwardsItsTimeoutToTheDial(t *testing.T) {
	t.Parallel()

	addr := liveListener(t)
	for _, r := range drain(Run(context.Background(), []Target{{Alias: "x", Addr: addr}}, time.Nanosecond)) {
		if r.Up {
			t.Fatalf("%s reported Up with a 1ns timeout against a live listener", r.Alias)
		}
	}
}

// TestRunForwardsItsContextToTheDial pins the other half of the same
// delegation. The timeout is generous, so an already-canceled context is the
// only thing that can stop a live listener from answering.
//
// The loop asserts nothing is Up and deliberately does NOT assert that a
// result arrives. Under a canceled context the goroutine has to win two
// independent coin flips to deliver one: the semaphore select and the send
// select each have both arms ready -- a free slot or a live receiver on one
// side, a closed Done on the other -- and Go picks uniformly at random. So a
// result should arrive about 1 time in 4, and measured 114/400 on this
// machine. `len(got) != 1` would therefore fail roughly 70% of runs against
// CORRECT code, which is the worst kind of flake: it fails in the direction
// that looks like a real bug. Assert only on what does arrive.
func TestRunForwardsItsContextToTheDial(t *testing.T) {
	t.Parallel()

	addr := liveListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, r := range drain(Run(ctx, []Target{{Alias: "x", Addr: addr}}, 5*time.Second)) {
		if r.Up {
			t.Fatalf("%s reported Up under an already-canceled context", r.Alias)
		}
	}
}

func TestRunWithNoTargetsClosesImmediately(t *testing.T) {
	select {
	case _, open := <-Run(context.Background(), nil, time.Second):
		if open {
			t.Fatal("received a result for zero targets")
		}
	case <-time.After(time.Second):
		t.Fatal("channel never closed")
	}
}

// Latency is measured around the dial itself, so it reflects the time the
// connection attempt took even when the attempt failed. The picker only shows
// it for a host that answered, but the measurement must not depend on that.
func TestRunReportsDialLatency(t *testing.T) {
	dial := func(ctx context.Context, timeout time.Duration, addr string) (net.Conn, error) {
		time.Sleep(20 * time.Millisecond)
		return nil, errors.New("refused")
	}
	for r := range run(context.Background(), []Target{{Alias: "h", Addr: "x"}}, time.Second, dial) {
		if r.Latency < 20*time.Millisecond {
			t.Fatalf("latency = %v, want at least the 20ms the dial took", r.Latency)
		}
	}
}
