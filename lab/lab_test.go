package lab

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var workerBinary string

func TestMain(m *testing.M) {
	cleanup := func() {}
	if os.Getenv("BOOK_LAB_DSN") != "" {
		dir, err := os.MkdirTemp("", "workflow-worker-")
		if err != nil {
			panic(err)
		}
		cleanup = func() { os.RemoveAll(dir) }
		workerBinary = filepath.Join(dir, "worker")
		command := exec.Command("go", "build", "-race", "-o", workerBinary, "../cmd/worker")
		if output, err := command.CombinedOutput(); err != nil {
			fmt.Fprintln(os.Stderr, string(output))
			cleanup()
			os.Exit(2)
		}
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func db(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("BOOK_LAB_DSN") == "" {
		t.Skip("real PostgreSQL integration: set BOOK_LAB_DSN or use repository CI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := Open(ctx, "wf_"+ID(), true)
	must(t, err)
	t.Cleanup(func() { must(t, s.Close()) })
	return s
}

var proposer = Actor{Tenant: "tenant-a", Subject: "employee-a", Role: "proposer"}
var approver = Actor{Tenant: "tenant-a", Subject: "manager-a", Role: "approver"}

func sample(idempotent bool) Plan {
	return Plan{Version: 1, Action: "issue_credit", Account: "account-a", AmountMinor: 500, Currency: "CNY", Idempotent: idempotent}
}
func approved(t *testing.T, s *Store, idempotent bool) Workflow {
	t.Helper()
	ctx := context.Background()
	w, err := s.Create(ctx, proposer, ID(), sample(idempotent))
	must(t, err)
	must(t, s.Approve(ctx, approver, w.ID, w.Fingerprint, time.Minute))
	w, err = s.Get(ctx, w.ID)
	must(t, err)
	return w
}
func effects(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	must(t, s.DB.QueryRow("SELECT count(*) FROM tool_effects").Scan(&n))
	return n
}
func state(t *testing.T, s *Store, id string) Workflow {
	t.Helper()
	w, err := s.Get(context.Background(), id)
	must(t, err)
	return w
}
func observe(t *testing.T, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	must(t, err)
	t.Log("BOOK_OBSERVATION " + string(b))
}
func fixture(t *testing.T, s *Store, idempotent bool) (*ToolService, *ToolClient, *Worker) {
	t.Helper()
	server := NewTool(s.DB, idempotent)
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	client, err := NewToolClient(httpServer.URL)
	must(t, err)
	t.Cleanup(client.Close)
	return server, client, &Worker{Store: s, Tool: client, Name: "worker-a", Lease: 3 * time.Second, Backoff: 20 * time.Millisecond}
}
func expired(t *testing.T, s *Store, id string) {
	t.Helper()
	_, err := s.DB.Exec("UPDATE workflows SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", id)
	must(t, err)
}
func due(t *testing.T, s *Store, id string) {
	t.Helper()
	_, err := s.DB.Exec("UPDATE workflows SET ready_at=clock_timestamp()-interval '1 second' WHERE id=$1", id)
	must(t, err)
}
func until(t *testing.T, budget time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("bounded condition did not become true")
}
func TestProposalCannotGrantAuthority(t *testing.T) {
	base := `{"version":1,"action":"issue_credit","account":"account-a","amount_minor":500,"currency":"CNY"}`
	contract := ToolContract{Name: "credit-service", Idempotency: true, AuthoritativeLookup: true}
	p, err := AdmitProposal([]byte(base), contract)
	must(t, err)
	if !p.Idempotent {
		t.Fatal("capability must come from trusted registry")
	}
	for _, extra := range []string{`"approved":true`, `"idempotent":true`, `"tenant":"victim"`, `"endpoint":"http://evil.invalid"`, `"actor":"admin"`} {
		_, err = AdmitProposal([]byte(strings.TrimSuffix(base, "}")+","+extra+"}"), contract)
		if err == nil {
			t.Fatal("accepted model authority", extra)
		}
	}
	if _, err = AdmitProposal([]byte(base+base), contract); err == nil {
		t.Fatal("accepted two proposals")
	}
	if _, err = AdmitProposal([]byte(strings.Repeat(" ", 4097)), contract); err == nil {
		t.Fatal("unbounded proposal")
	}
	observe(t, map[string]any{"authority_fields_rejected": 5, "capability_from_registry": p.Idempotent})
}
func TestOperationKeyHasUnambiguousTenantScope(t *testing.T) {
	a := Workflow{Tenant: "a:b", Operation: "c"}
	b := Workflow{Tenant: "a", Operation: "b:c"}
	if a.EffectKey() == b.EffectKey() {
		t.Fatal("ambiguous compound identity")
	}
	if a.EffectKey() != (Workflow{Tenant: "a:b", Operation: "c"}).EffectKey() {
		t.Fatal("unstable identity")
	}
}
func TestOperationIdentityRejectsParameterDrift(t *testing.T) {
	s := db(t)
	ctx := context.Background()
	a, err := s.Create(ctx, proposer, "same-operation", sample(true))
	must(t, err)
	b, err := s.Create(ctx, proposer, "same-operation", sample(true))
	must(t, err)
	if a.ID != b.ID {
		t.Fatal("duplicate workflow")
	}
	changed := sample(true)
	changed.AmountMinor = 501
	if _, err = s.Create(ctx, proposer, "same-operation", changed); !errors.Is(err, ErrConflict) {
		t.Fatal("parameter drift accepted", err)
	}
	observe(t, map[string]any{"same_workflow": a.ID == b.ID, "drift_rejected": true, "state": state(t, s, a.ID).State})
}
func TestApprovalBindsTenantRoleAndFingerprint(t *testing.T) {
	s := db(t)
	ctx := context.Background()
	w, err := s.Create(ctx, proposer, ID(), sample(true))
	must(t, err)
	for _, a := range []Actor{{Tenant: "tenant-b", Subject: "x", Role: "approver"}, proposer} {
		if !errors.Is(s.Approve(ctx, a, w.ID, w.Fingerprint, time.Minute), ErrDenied) {
			t.Fatal("unauthorized approval")
		}
	}
	if !errors.Is(s.Approve(ctx, approver, w.ID, "different-fingerprint", time.Minute), ErrDenied) {
		t.Fatal("unbound approval")
	}
	must(t, s.Approve(ctx, approver, w.ID, w.Fingerprint, time.Minute))
	if state(t, s, w.ID).State != "ready" {
		t.Fatal("approval not persisted")
	}
}
func TestTwoClaimersCannotOwnOneWorkflow(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var lock sync.Mutex
	claims := []Workflow{}
	failures := []error{}
	for _, name := range []string{"a", "b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			<-start
			w, err := s.Claim(context.Background(), owner, time.Second)
			lock.Lock()
			defer lock.Unlock()
			if err != nil {
				failures = append(failures, err)
			}
			if w != nil {
				claims = append(claims, *w)
			}
		}(name)
	}
	close(start)
	wg.Wait()
	if len(failures) > 0 || len(claims) != 1 || claims[0].ID != job.ID {
		t.Fatal(claims, failures)
	}
	observe(t, map[string]any{"concurrent_claimers": 2, "successful_claims": len(claims), "fence": claims[0].Fence})
}
func TestHeartbeatCannotReviveAnExpiredLease(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	w, err := s.Claim(context.Background(), "owner", time.Second)
	must(t, err)
	before := w.LeaseUntil
	must(t, s.Heartbeat(context.Background(), *w, 2*time.Second))
	after := state(t, s, job.ID).LeaseUntil
	if !after.After(*before) {
		t.Fatal("heartbeat did not extend lease")
	}
	expired(t, s, job.ID)
	if !errors.Is(s.Heartbeat(context.Background(), *w, time.Second), ErrStale) {
		t.Fatal("expired owner revived")
	}
}
func TestExpiredOwnerCannotCommitAnOldResult(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	_, client, _ := fixture(t, s, true)
	first, err := s.Claim(context.Background(), "old", time.Second)
	must(t, err)
	expired(t, s, job.ID)
	n, err := s.RecoverExpired(context.Background())
	must(t, err)
	if n != 1 {
		t.Fatal(n)
	}
	must(t, (&Worker{Store: s, Tool: client}).Reconcile(context.Background(), job.ID))
	second, err := s.Claim(context.Background(), "new", time.Second)
	must(t, err)
	receipt, err := client.Call(context.Background(), *second)
	must(t, err)
	if err = s.Complete(context.Background(), *first, receipt); !errors.Is(err, ErrStale) {
		t.Fatal("stale owner accepted", err)
	}
	again, err := client.Call(context.Background(), *second)
	must(t, err)
	must(t, s.Complete(context.Background(), *second, again))
	if again.ID != receipt.ID || effects(t, s) != 1 {
		t.Fatal("duplicate effect")
	}
	observe(t, map[string]any{"old_fence": first.Fence, "new_fence": second.Fence, "effects": effects(t, s), "state": state(t, s, job.ID).State})
}
func TestCommitReplyLostBecomesUnknownThenReconciled(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, _, worker := fixture(t, s, true)
	server.DropResponses.Store(true)
	must(t, worker.RunOne(context.Background()))
	unknown := state(t, s, job.ID)
	if unknown.State != "unknown" || effects(t, s) != 1 {
		t.Fatal(unknown.State)
	}
	must(t, worker.Reconcile(context.Background(), job.ID))
	final := state(t, s, job.ID)
	if final.State != "succeeded" || effects(t, s) != 1 {
		t.Fatal(final.State)
	}
	observe(t, map[string]any{"after_reply_loss": unknown.State, "after_lookup": final.State, "effects": effects(t, s), "post_calls": server.Calls.Load()})
}
func TestNonIdempotentUnknownNeverAutomaticallyReplays(t *testing.T) {
	s := db(t)
	job := approved(t, s, false)
	server, _, worker := fixture(t, s, false)
	server.DropResponses.Store(true)
	must(t, worker.RunOne(context.Background()))
	if state(t, s, job.ID).State != "unknown" || effects(t, s) != 1 {
		t.Fatal("missing unknown effect")
	}
	if !errors.Is(worker.Reconcile(context.Background(), job.ID), ErrManual) {
		t.Fatal("unqueryable tool automatically resolved")
	}
	for i := 0; i < 3; i++ {
		must(t, worker.RunOne(context.Background()))
	}
	if effects(t, s) != 1 || server.Calls.Load() != 1 {
		t.Fatal("unknown replayed")
	}
	observe(t, map[string]any{"state": state(t, s, job.ID).State, "effects": effects(t, s), "post_calls": server.Calls.Load()})
}

// process owns an OS process, not a goroutine pretending to crash a worker.
type process struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	lines  chan string
	done   chan struct{}
	err    error
	stderr bytes.Buffer
}

func child(t *testing.T, s *Store, c *ToolClient, name string, extra ...string) *process {
	t.Helper()
	args := append([]string{"-schema", s.Schema, "-tool", c.Base, "-name", name}, extra...)
	p := &process{cmd: exec.Command(workerBinary, args...), lines: make(chan string, 16), done: make(chan struct{})}
	p.cmd.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0")
	var err error
	p.input, err = p.cmd.StdinPipe()
	must(t, err)
	output, err := p.cmd.StdoutPipe()
	must(t, err)
	p.cmd.Stderr = &p.stderr
	must(t, p.cmd.Start())
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			p.lines <- scanner.Text()
		}
		close(p.lines)
	}()
	go func() { <-scanned; p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			<-p.done
		}
		_ = p.input.Close()
	})
	return p
}
func (p *process) stage(t *testing.T, prefix string) string {
	t.Helper()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				<-p.done
				t.Fatalf("process ended before %q: %v %s", prefix, p.err, p.stderr.String())
			}
			if strings.HasPrefix(line, prefix) {
				return line
			}
		case <-timer.C:
			t.Fatalf("process timeout waiting for %s", prefix)
		}
	}
}
func (p *process) release(t *testing.T) {
	t.Helper()
	_, err := io.WriteString(p.input, "\n")
	must(t, err)
}
func (p *process) finish(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		if p.err != nil {
			t.Fatalf("worker failed: %v %s", p.err, p.stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not exit")
	}
}
func (p *process) kill(t *testing.T) {
	t.Helper()
	must(t, p.cmd.Process.Kill())
	<-p.done
	if p.err == nil {
		t.Fatal("SIGKILL did not terminate worker")
	}
}
func recoverByElapsedLease(t *testing.T, s *Store, id string) {
	t.Helper()
	// Polling the real PostgreSQL clock lets the actual three-second lease expire.
	until(t, 5*time.Second, func() bool {
		var expired bool
		must(t, s.DB.QueryRow("SELECT lease_until<=clock_timestamp() FROM workflows WHERE id=$1", id).Scan(&expired))
		return expired
	})
	n, err := s.RecoverExpired(context.Background())
	must(t, err)
	if n != 1 {
		t.Fatalf("recovered %d", n)
	}
	if state(t, s, id).State != "unknown" {
		t.Fatal("expired lease skipped unknown")
	}
}
func TestTwoWorkerProcessesClaimExactlyOne(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, client, _ := fixture(t, s, true)
	a := child(t, s, client, "process-a", "-start-gate", "-pause", "after_claim")
	b := child(t, s, client, "process-b", "-start-gate", "-pause", "after_claim")
	a.stage(t, "BOOK_READY")
	b.stage(t, "BOOK_READY")
	a.release(t)
	b.release(t)
	until(t, 5*time.Second, func() bool { return state(t, s, job.ID).State == "running" })
	current := state(t, s, job.ID)
	winner, loser := a, b
	if current.Owner == "process-b" {
		winner, loser = b, a
	}
	winner.stage(t, "BOOK_STAGE after_claim")
	loser.finish(t)
	winner.release(t)
	winner.finish(t)
	var claims int
	must(t, s.DB.QueryRow("SELECT count(*) FROM events WHERE kind='claimed'").Scan(&claims))
	if claims != 1 || effects(t, s) != 1 || server.Calls.Load() != 1 || state(t, s, job.ID).State != "succeeded" {
		t.Fatal("duplicate process execution")
	}
	observe(t, map[string]any{"os_processes": 2, "claims": claims, "effects": effects(t, s), "state": state(t, s, job.ID).State})
}
func TestSIGKILLAfterClaimQueries404AndRestarts(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, client, _ := fixture(t, s, true)
	p := child(t, s, client, "crashed", "-pause", "after_claim")
	p.stage(t, "BOOK_STAGE after_claim")
	p.kill(t)
	if effects(t, s) != 0 {
		t.Fatal("effect before dispatch")
	}
	recoverByElapsedLease(t, s, job.ID)
	next := child(t, s, client, "restarted", "-reconcile", job.ID)
	next.finish(t)
	w := state(t, s, job.ID)
	if w.State != "succeeded" || w.Attempt != 2 || effects(t, s) != 1 {
		t.Fatal(w)
	}
	observe(t, map[string]any{"signal": "SIGKILL", "window": "after_claim", "lease_expiry": "elapsed PostgreSQL clock; 3s lease", "state": w.State, "attempts": w.Attempt, "effects": effects(t, s), "post_calls": server.Calls.Load()})
}
func TestSIGKILLAfterToolReconcilesExistingEffect(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, client, _ := fixture(t, s, true)
	p := child(t, s, client, "crashed", "-pause", "after_tool")
	p.stage(t, "BOOK_STAGE after_tool")
	p.kill(t)
	if effects(t, s) != 1 || state(t, s, job.ID).State != "running" {
		t.Fatal("wrong crash window")
	}
	recoverByElapsedLease(t, s, job.ID)
	next := child(t, s, client, "restarted", "-reconcile", job.ID)
	next.finish(t)
	w := state(t, s, job.ID)
	if w.State != "succeeded" || w.Attempt != 1 || effects(t, s) != 1 || server.Calls.Load() != 1 {
		t.Fatal(w)
	}
	observe(t, map[string]any{"signal": "SIGKILL", "window": "after_tool", "lease_expiry": "elapsed PostgreSQL clock; 3s lease", "state": w.State, "attempts": w.Attempt, "effects": effects(t, s), "post_calls": server.Calls.Load()})
}
func TestSIGKILLNonIdempotentRequiresManualReview(t *testing.T) {
	for _, window := range []string{"after_claim", "after_tool"} {
		t.Run(window, func(t *testing.T) {
			s := db(t)
			job := approved(t, s, false)
			server, client, worker := fixture(t, s, false)
			p := child(t, s, client, "crashed", "-pause", window)
			p.stage(t, "BOOK_STAGE "+window)
			p.kill(t)
			recoverByElapsedLease(t, s, job.ID)
			if !errors.Is(worker.Reconcile(context.Background(), job.ID), ErrManual) {
				t.Fatal("unsafe reconciliation")
			}
			next := child(t, s, client, "restarted")
			next.finish(t)
			want := 0
			if window == "after_tool" {
				want = 1
			}
			if state(t, s, job.ID).State != "unknown" || effects(t, s) != want || server.Calls.Load() != int64(want) {
				t.Fatal("non-idempotent replay")
			}
			observe(t, map[string]any{"signal": "SIGKILL", "window": window, "lease_expiry": "elapsed PostgreSQL clock; 3s lease", "state": "unknown", "effects": effects(t, s), "post_calls": server.Calls.Load(), "manual_review": true})
		})
	}
}
func TestApprovalExpiresWhileQueued(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, _, worker := fixture(t, s, true)
	_, err := s.DB.Exec("UPDATE workflows SET approval_expires=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID)
	must(t, err)
	must(t, worker.RunOne(context.Background()))
	if state(t, s, job.ID).State != "waiting_approval" || server.Calls.Load() != 0 {
		t.Fatal("expired approval dispatched")
	}
	observe(t, map[string]any{"expiry": "SQL-injected before claim", "state": state(t, s, job.ID).State, "post_calls": server.Calls.Load()})
}
func TestApprovalExpiresAfterClaim(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, _, worker := fixture(t, s, true)
	worker.Hook = func(stage string, w Workflow) {
		if stage == "after_claim" {
			_, err := s.DB.Exec("UPDATE workflows SET approval_expires=clock_timestamp()-interval '1 second' WHERE id=$1", w.ID)
			must(t, err)
		}
	}
	must(t, worker.RunOne(context.Background()))
	if state(t, s, job.ID).State != "waiting_approval" || server.Calls.Load() != 0 {
		t.Fatal("dispatch ignored approval expiry")
	}
	observe(t, map[string]any{"expiry": "SQL-injected after claim", "state": state(t, s, job.ID).State, "effects": effects(t, s)})
}
func TestCancelBeforeDispatch(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, _, worker := fixture(t, s, true)
	worker.Hook = func(stage string, w Workflow) {
		if stage == "after_claim" {
			must(t, s.Cancel(context.Background(), proposer, w.ID))
		}
	}
	must(t, worker.RunOne(context.Background()))
	if state(t, s, job.ID).State != "cancelled" || server.Calls.Load() != 0 {
		t.Fatal("cancelled work dispatched")
	}
	queued := approved(t, s, true)
	must(t, s.Cancel(context.Background(), proposer, queued.ID))
	must(t, worker.RunOne(context.Background()))
	if state(t, s, queued.ID).State != "cancelled" || server.Calls.Load() != 0 {
		t.Fatal("queued cancellation ignored")
	}
	observe(t, map[string]any{"before_dispatch": "cancelled", "queued": "cancelled", "effects": effects(t, s)})
}
func TestCancelAfterEffectPreservesSucceededFact(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	_, _, worker := fixture(t, s, true)
	worker.Hook = func(stage string, w Workflow) {
		if stage == "after_tool" {
			must(t, s.Cancel(context.Background(), proposer, w.ID))
		}
	}
	must(t, worker.RunOne(context.Background()))
	w := state(t, s, job.ID)
	if w.State != "succeeded" || !w.CancelRequested || effects(t, s) != 1 {
		t.Fatal("cancellation erased an effect")
	}
	observe(t, map[string]any{"state": w.State, "cancel_requested": w.CancelRequested, "effects": effects(t, s), "compensated": false})
}
func TestTrusted429UsesTimedRetry(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, _, worker := fixture(t, s, true)
	worker.Backoff = 250 * time.Millisecond
	server.RejectRemaining.Store(1)
	must(t, worker.RunOne(context.Background()))
	if state(t, s, job.ID).State != "ready" || effects(t, s) != 0 {
		t.Fatal("429 not safely queued")
	}
	must(t, worker.RunOne(context.Background()))
	if server.Calls.Load() != 1 {
		t.Fatal("backoff ignored")
	}
	until(t, 2*time.Second, func() bool {
		var ready bool
		must(t, s.DB.QueryRow("SELECT ready_at<=clock_timestamp() FROM workflows WHERE id=$1", job.ID).Scan(&ready))
		return ready
	})
	must(t, worker.RunOne(context.Background()))
	w := state(t, s, job.ID)
	if w.State != "succeeded" || w.Attempt != 2 || effects(t, s) != 1 {
		t.Fatal(w)
	}
	observe(t, map[string]any{"backoff_ms": 250, "timer": "elapsed PostgreSQL clock", "attempts": w.Attempt, "post_calls": server.Calls.Load(), "effects": effects(t, s)})
}
func TestTrusted429ExhaustsPersistentBudget(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, _, worker := fixture(t, s, true)
	worker.Backoff = 0
	server.RejectRemaining.Store(20)
	for i := 0; i < 8; i++ {
		must(t, worker.RunOne(context.Background()))
	}
	w := state(t, s, job.ID)
	if w.State != "failed" || w.Attempt != 3 || w.MaxAttempts != 3 || server.Calls.Load() != 3 || effects(t, s) != 0 {
		t.Fatal(w)
	}
	observe(t, map[string]any{"state": w.State, "persistent_budget": w.MaxAttempts, "attempts": w.Attempt, "post_calls": server.Calls.Load(), "effects": effects(t, s)})
}
func TestUnknown404UsesOriginalKeyAndStopsAtBudget(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	_, _, worker := fixture(t, s, true)
	key := job.EffectKey()
	// An authoritative empty lookup is exercised over HTTP on each lost claim.
	for i := 1; i <= 3; i++ {
		claim, err := s.Claim(context.Background(), "lost", time.Second)
		must(t, err)
		if claim == nil {
			t.Fatal("missing claim")
		}
		must(t, s.Unknown(context.Background(), *claim))
		err = worker.Reconcile(context.Background(), job.ID)
		if i < 3 {
			must(t, err)
			if state(t, s, job.ID).EffectKey() != key {
				t.Fatal("key changed")
			}
		} else if !errors.Is(err, ErrManual) {
			t.Fatal("budget reset by lookup", err)
		}
	}
	for i := 0; i < 4; i++ {
		must(t, worker.RunOne(context.Background()))
	}
	w := state(t, s, job.ID)
	if w.State != "unknown" || w.Attempt != 3 || effects(t, s) != 0 {
		t.Fatal(w)
	}
	observe(t, map[string]any{"lookup": "HTTP 404", "original_key_preserved": w.EffectKey() == key, "attempts": w.Attempt, "persistent_budget": w.MaxAttempts, "state": w.State, "effects": effects(t, s)})
}
func TestPersistedVersionTwoRejectedByOldWorker(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, client, _ := fixture(t, s, true)
	_, err := s.DB.Exec("UPDATE workflows SET plan_version=2,plan=jsonb_set(plan,'{version}','2') WHERE id=$1", job.ID)
	must(t, err)
	p := child(t, s, client, "old-worker")
	p.finish(t)
	w := state(t, s, job.ID)
	if w.State != "incompatible" || w.Attempt != 0 || server.Calls.Load() != 0 {
		t.Fatal("unsupported version executed")
	}
	observe(t, map[string]any{"persisted_version": 2, "worker_version": 1, "state": w.State, "post_calls": server.Calls.Load()})
}
func TestTerminalTransactionRollsBackButEffectCanBeReconciled(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	server, _, worker := fixture(t, s, true)
	_, err := s.DB.Exec("ALTER TABLE events ADD CONSTRAINT reject_completed CHECK (kind <> 'completed')")
	must(t, err)
	if err = worker.RunOne(context.Background()); err == nil {
		t.Fatal("injected terminal transaction accepted")
	}
	var completed int
	must(t, s.DB.QueryRow("SELECT count(*) FROM events WHERE kind='completed'").Scan(&completed))
	before := state(t, s, job.ID)
	if before.State != "running" || before.Result != nil || completed != 0 || effects(t, s) != 1 {
		t.Fatal("terminal update not atomic")
	}
	_, err = s.DB.Exec("ALTER TABLE events DROP CONSTRAINT reject_completed")
	must(t, err)
	expired(t, s, job.ID)
	_, err = s.RecoverExpired(context.Background())
	must(t, err)
	must(t, worker.Reconcile(context.Background(), job.ID))
	after := state(t, s, job.ID)
	if after.State != "succeeded" || effects(t, s) != 1 || server.Calls.Load() != 1 {
		t.Fatal("reconciliation replayed effect")
	}
	observe(t, map[string]any{"fault": "events CHECK rejects completed", "after_rollback": before.State, "result_rolled_back": before.Result == nil, "completed_events": completed, "lease_expiry": "SQL-injected", "after_reconcile": after.State, "effects": effects(t, s), "post_calls": server.Calls.Load()})
}
func TestReceiptFingerprintMismatchCannotCompleteOrReconcile(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	_, client, _ := fixture(t, s, true)
	claim, err := s.Claim(context.Background(), "owner", time.Second)
	must(t, err)
	receipt, err := client.Call(context.Background(), *claim)
	must(t, err)
	receipt.Fingerprint = "wrong"
	if s.Complete(context.Background(), *claim, receipt) == nil {
		t.Fatal("mismatched receipt completed")
	}
	must(t, s.Unknown(context.Background(), *claim))
	unknown := state(t, s, job.ID)
	if s.Reconciled(context.Background(), unknown, &receipt, false) == nil {
		t.Fatal("mismatched receipt reconciled")
	}
	if state(t, s, job.ID).State != "unknown" || effects(t, s) != 1 {
		t.Fatal("invalid proof changed state")
	}
	observe(t, map[string]any{"completion_rejected": true, "reconciliation_rejected": true, "state": "unknown", "effects": effects(t, s)})
}
func TestEventsExcludeRawSensitiveInputs(t *testing.T) {
	s := db(t)
	ctx := context.Background()
	p := sample(true)
	p.Account = "private-account-DO-NOT-LOG-81925"
	job, err := s.Create(ctx, proposer, ID(), p)
	must(t, err)
	must(t, s.Approve(ctx, approver, job.ID, job.Fingerprint, time.Minute))
	_, _, worker := fixture(t, s, true)
	must(t, worker.RunOne(ctx))
	rows, err := s.DB.Query("SELECT kind,detail FROM events ORDER BY seq")
	must(t, err)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var kind, detail string
		must(t, rows.Scan(&kind, &detail))
		for _, secret := range []string{p.Account, DevelopmentToolToken, "amount_minor", "Authorization", "book-lab-only"} {
			if strings.Contains(kind+detail, secret) {
				t.Fatalf("raw sensitive event content: %s", secret)
			}
		}
		n++
	}
	must(t, rows.Err())
	if n < 4 {
		t.Fatal("missing audit events")
	}
	observe(t, map[string]any{"events_checked": n, "raw_sensitive_markers_found": 0, "scope": "events kind/detail; workflow and tool ledger intentionally retain business fields"})
}
func TestCancellationDuringLookupCannotRequeue(t *testing.T) {
	s := db(t)
	job := approved(t, s, true)
	ctx := context.Background()
	claim, err := s.Claim(ctx, "owner", time.Second)
	must(t, err)
	must(t, s.Unknown(ctx, *claim))
	// The lookup handler authenticates its request, then cancellation wins before 404 returns.
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+DevelopmentToolToken {
			t.Error("missing auth")
		}
		must(t, s.Cancel(ctx, proposer, job.ID))
		http.NotFound(w, r)
	}))
	defer httpServer.Close()
	client, err := NewToolClient(httpServer.URL)
	must(t, err)
	defer client.Close()
	worker := &Worker{Store: s, Tool: client}
	if !errors.Is(worker.Reconcile(ctx, job.ID), ErrManual) {
		t.Fatal("snapshot cancellation race allowed requeue")
	}
	w := state(t, s, job.ID)
	if w.State != "unknown" || !w.CancelRequested {
		t.Fatal(w)
	}
	observe(t, map[string]any{"cancel_during": "HTTP lookup", "lookup": "404", "state": w.State, "cancel_requested": w.CancelRequested, "effects": effects(t, s)})
}
