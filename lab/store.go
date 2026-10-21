// Package lab implements a deliberately bounded, real-PostgreSQL workflow lab.
package lab

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schemaSQL string
var schemaName = regexp.MustCompile(`^wf_[0-9a-f]{16}$`)
var (
	ErrConflict = errors.New("operation key reused with another plan")
	ErrStale    = errors.New("owner, fence, state or lease is no longer valid")
	ErrDenied   = errors.New("actor or approval binding rejected")
	ErrManual   = errors.New("outcome needs independent manual reconciliation")
)

type Plan struct {
	Version     int    `json:"version"`
	Action      string `json:"action"`
	Account     string `json:"account"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Idempotent  bool   `json:"idempotent"`
}

func (p Plan) Validate() error {
	if p.Version != 1 || p.Action != "issue_credit" || p.Account == "" || len(p.Account) > 80 || p.AmountMinor < 1 || p.AmountMinor > 10000 || p.Currency != "CNY" {
		return errors.New("proposal is outside the executable contract")
	}
	return nil
}
func (p Plan) Fingerprint() string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func ID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type Actor struct{ Tenant, Subject, Role string }
type Workflow struct {
	ID, Tenant, Operation, Fingerprint, State, Owner string
	Plan                                             Plan
	Fence                                            int64
	Attempt, MaxAttempts                             int
	LeaseUntil, ApprovalExpires                      *time.Time
	CancelRequested                                  bool
	Result                                           *Receipt
}
type Store struct {
	DB     *sql.DB
	Schema string
	owned  bool
}

func Open(ctx context.Context, schema string, create bool) (*Store, error) {
	dsn := os.Getenv("BOOK_LAB_DSN")
	if dsn == "" {
		dsn = "postgres://book:book-lab-only@127.0.0.1:25432/book?sslmode=disable"
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.Host != "localhost" {
		ip := net.ParseIP(cfg.Host)
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("lab only permits a loopback PostgreSQL host")
		}
	}
	if !schemaName.MatchString(schema) {
		return nil, errors.New("invalid isolated schema")
	}
	if create {
		admin := stdlib.OpenDB(*cfg)
		defer admin.Close()
		if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
			return nil, err
		}
	}
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(12)
	s := &Store{DB: db, Schema: schema, owned: create}
	if create {
		for _, stmt := range strings.Split(schemaSQL, ";") {
			if strings.TrimSpace(stmt) != "" {
				if _, err = db.ExecContext(ctx, stmt); err != nil {
					s.Close()
					return nil, err
				}
			}
		}
	}
	if err = db.PingContext(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error {
	var err error
	if s.owned {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err = s.DB.ExecContext(ctx, "DROP SCHEMA "+s.Schema+" CASCADE")
	}
	closeErr := s.DB.Close()
	if err != nil {
		return err
	}
	return closeErr
}

const DefaultMaxAttempts = 3

const columns = "id,tenant,operation,plan,fingerprint,state,owner,fence,attempt,max_attempts,lease_until,approval_expires,cancel_requested,result"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Workflow, error) {
	var w Workflow
	var raw, result []byte
	err := row.Scan(&w.ID, &w.Tenant, &w.Operation, &raw, &w.Fingerprint, &w.State, &w.Owner, &w.Fence, &w.Attempt, &w.MaxAttempts, &w.LeaseUntil, &w.ApprovalExpires, &w.CancelRequested, &result)
	if err != nil {
		return w, err
	}
	if err = json.Unmarshal(raw, &w.Plan); err != nil {
		return w, err
	}
	if result != nil {
		var r Receipt
		if err = json.Unmarshal(result, &r); err != nil {
			return w, err
		}
		w.Result = &r
	}
	return w, nil
}
func (s *Store) Get(ctx context.Context, id string) (Workflow, error) {
	return scan(s.DB.QueryRowContext(ctx, "SELECT "+columns+" FROM workflows WHERE id=$1", id))
}
func event(ctx context.Context, tx *sql.Tx, id, kind string, fence int64, detail string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO events(workflow_id,kind,fence,detail) VALUES($1,$2,$3,$4)", id, kind, fence, detail)
	return err
}
func (s *Store) Create(ctx context.Context, a Actor, op string, p Plan) (Workflow, error) {
	if a.Tenant == "" || len(a.Tenant) > 80 || a.Subject == "" || op == "" || len(op) > 120 {
		return Workflow{}, ErrDenied
	}
	if err := p.Validate(); err != nil {
		return Workflow{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Workflow{}, err
	}
	defer tx.Rollback()
	raw, _ := json.Marshal(p)
	id := ID()
	result, err := tx.ExecContext(ctx, `INSERT INTO workflows(id,tenant,operation,plan,fingerprint,plan_version,state) VALUES($1,$2,$3,$4,$5,$6,'waiting_approval') ON CONFLICT(tenant,operation) DO NOTHING`, id, a.Tenant, op, raw, p.Fingerprint(), p.Version)
	if err != nil {
		return Workflow{}, err
	}
	n, _ := result.RowsAffected()
	w, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM workflows WHERE tenant=$1 AND operation=$2", a.Tenant, op))
	if err != nil {
		return w, err
	}
	if w.Fingerprint != p.Fingerprint() {
		return w, ErrConflict
	}
	if n == 1 {
		if err = event(ctx, tx, w.ID, "proposed", 0, "untrusted proposal validated; awaiting approval"); err != nil {
			return w, err
		}
	}
	return w, tx.Commit()
}
func (s *Store) Approve(ctx context.Context, a Actor, id, fingerprint string, ttl time.Duration) error {
	if a.Role != "approver" || a.Subject == "" || ttl <= 0 || ttl > 24*time.Hour {
		return ErrDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var fence int64
	err = tx.QueryRowContext(ctx, `UPDATE workflows SET state='ready',approved_by=$1,approval_expires=clock_timestamp()+($2::bigint*interval '1 millisecond'),updated_at=clock_timestamp() WHERE id=$3 AND tenant=$4 AND state='waiting_approval' AND fingerprint=$5 RETURNING fence`, a.Subject, ttl.Milliseconds(), id, a.Tenant, fingerprint).Scan(&fence)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if err = event(ctx, tx, id, "approved", fence, "approval bound to immutable fingerprint"); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Claim(ctx context.Context, owner string, lease time.Duration) (*Workflow, error) {
	if owner == "" || lease < time.Millisecond || lease > time.Hour {
		return nil, ErrDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	w, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM workflows WHERE state='ready' AND ready_at<=clock_timestamp() ORDER BY ready_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1"))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT plan_version FROM workflows WHERE id=$1", w.ID).Scan(&version); err != nil {
		return nil, err
	}
	next, detail := "running", "claimed"
	if w.CancelRequested {
		next, detail = "cancelled", "cancelled_before_claim"
	} else if w.Attempt >= w.MaxAttempts {
		next, detail = "failed", "retry_budget_exhausted"
	} else if version != 1 {
		next, detail = "incompatible", "worker does not support persisted plan version"
	} else if w.ApprovalExpires == nil {
		next, detail = "waiting_approval", "approval missing"
	} else {
		var valid bool
		if err = tx.QueryRowContext(ctx, "SELECT approval_expires>clock_timestamp() FROM workflows WHERE id=$1", w.ID).Scan(&valid); err != nil {
			return nil, err
		}
		if !valid {
			next, detail = "waiting_approval", "approval expired before claim"
		}
	}
	if next != "running" {
		_, err = tx.ExecContext(ctx, "UPDATE workflows SET state=$1,fence=fence+1,owner='',lease_until=NULL,updated_at=clock_timestamp() WHERE id=$2", next, w.ID)
		if err != nil {
			return nil, err
		}
		if err = event(ctx, tx, w.ID, detail, w.Fence+1, ""); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	w, err = scan(tx.QueryRowContext(ctx, "UPDATE workflows SET state='running',owner=$1,fence=fence+1,attempt=attempt+1,lease_until=clock_timestamp()+($2::bigint*interval '1 millisecond'),updated_at=clock_timestamp() WHERE id=$3 RETURNING "+columns, owner, lease.Milliseconds(), w.ID))
	if err != nil {
		return nil, err
	}
	if err = event(ctx, tx, w.ID, "claimed", w.Fence, owner); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &w, nil
}
func (s *Store) Heartbeat(ctx context.Context, w Workflow, lease time.Duration) error {
	if lease < time.Millisecond || lease > time.Hour {
		return ErrDenied
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE workflows SET lease_until=clock_timestamp()+($1::bigint*interval '1 millisecond'),updated_at=clock_timestamp() WHERE id=$2 AND state='running' AND owner=$3 AND fence=$4 AND lease_until>clock_timestamp()`, lease.Milliseconds(), w.ID, w.Owner, w.Fence)
	return affected(r, err)
}
func affected(r sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStale
	}
	return nil
}
func (s *Store) BeforeCall(ctx context.Context, w Workflow) error {
	var valid bool
	err := s.DB.QueryRowContext(ctx, `SELECT state='running' AND owner=$1 AND fence=$2 AND lease_until>clock_timestamp() AND approval_expires>clock_timestamp() AND NOT cancel_requested FROM workflows WHERE id=$3`, w.Owner, w.Fence, w.ID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrStale
	}
	return nil
}
func (s *Store) transition(ctx context.Context, w Workflow, state, kind string, receipt *Receipt, delay time.Duration) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw any
	if receipt != nil {
		b, _ := json.Marshal(receipt)
		raw = b
	}
	r, err := tx.ExecContext(ctx, `UPDATE workflows SET state=$1,result=$2,owner='',lease_until=NULL,fence=fence+1,ready_at=clock_timestamp()+($3::bigint*interval '1 millisecond'),updated_at=clock_timestamp() WHERE id=$4 AND state='running' AND owner=$5 AND fence=$6 AND lease_until>clock_timestamp()`, state, raw, delay.Milliseconds(), w.ID, w.Owner, w.Fence)
	if err = affected(r, err); err != nil {
		return err
	}
	if err = event(ctx, tx, w.ID, kind, w.Fence+1, ""); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Complete(ctx context.Context, w Workflow, r Receipt) error {
	if err := r.Validate(w); err != nil {
		return err
	}
	return s.transition(ctx, w, "succeeded", "completed", &r, 0)
}
func (s *Store) Unknown(ctx context.Context, w Workflow) error {
	return s.transition(ctx, w, "unknown", "outcome_unknown", nil, 0)
}
func (s *Store) Retry(ctx context.Context, w Workflow, delay time.Duration) error {
	if w.Attempt >= w.MaxAttempts {
		return s.transition(ctx, w, "failed", "retry_budget_exhausted", nil, 0)
	}
	return s.transition(ctx, w, "ready", "retry_scheduled", nil, delay)
}
func (s *Store) Hold(ctx context.Context, w Workflow) error {
	return s.transition(ctx, w, "waiting_approval", "dispatch_permission_expired", nil, 0)
}
func (s *Store) RecoverExpired(ctx context.Context) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM workflows WHERE state='running' AND lease_until<=clock_timestamp() FOR UPDATE SKIP LOCKED")
	if err != nil {
		return 0, err
	}
	var lost []Workflow
	for rows.Next() {
		w, e := scan(rows)
		if e != nil {
			rows.Close()
			return 0, e
		}
		lost = append(lost, w)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, w := range lost {
		// An expired lease proves lost ownership, not absence of an effect.
		// Even an idempotent tool must be queried before a new dispatch.
		next := "unknown"
		if _, err = tx.ExecContext(ctx, "UPDATE workflows SET state=$1,owner='',lease_until=NULL,fence=fence+1,ready_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$2", next, w.ID); err != nil {
			return 0, err
		}
		if err = event(ctx, tx, w.ID, "expired_lease_recovered", w.Fence+1, next); err != nil {
			return 0, err
		}
	}
	return len(lost), tx.Commit()
}
func (s *Store) Cancel(ctx context.Context, a Actor, id string) error {
	if a.Subject == "" {
		return ErrDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var fence int64
	var state string
	err = tx.QueryRowContext(ctx, `UPDATE workflows SET cancel_requested=true,state=CASE WHEN state IN ('waiting_approval','ready') THEN 'cancelled' ELSE state END,updated_at=clock_timestamp() WHERE id=$1 AND tenant=$2 RETURNING fence,state`, id, a.Tenant).Scan(&fence, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if err = event(ctx, tx, id, "cancel_requested", fence, "observed state="+state); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Reconciled(ctx context.Context, w Workflow, r *Receipt, requeue bool) error {
	if r != nil {
		if err := r.Validate(w); err != nil {
			return err
		}
	}
	if (requeue && r != nil) || (!requeue && r == nil) {
		return ErrManual
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Lock and inspect the current cancellation and budget, not the snapshot
	// taken before an HTTP lookup. Cancellation may arrive during that lookup.
	current, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM workflows WHERE id=$1 FOR UPDATE", w.ID))
	if err != nil {
		return err
	}
	if current.State != "unknown" || current.Fence != w.Fence {
		return ErrStale
	}
	state, kind := "succeeded", "reconciled_existing_effect"
	var raw any
	if requeue {
		if !current.Plan.Idempotent || current.CancelRequested || current.Attempt >= current.MaxAttempts {
			return ErrManual
		}
		state, kind = "ready", "reconciled_retry_same_key"
	} else {
		if err = r.Validate(current); err != nil {
			return err
		}
		b, _ := json.Marshal(r)
		raw = b
	}
	_, err = tx.ExecContext(ctx, `UPDATE workflows SET state=$1,result=$2,fence=fence+1,ready_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$3`, state, raw, w.ID)
	if err != nil {
		return err
	}
	if err = event(ctx, tx, w.ID, kind, w.Fence+1, ""); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Counts(ctx context.Context) (map[string]int, error) {
	result := map[string]int{}
	rows, err := s.DB.QueryContext(ctx, "SELECT state,count(*) FROM workflows GROUP BY state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var n int
		if err = rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		result[name] = n
	}
	return result, rows.Err()
}
func (w Workflow) EffectKey() string {
	raw, _ := json.Marshal([]string{w.Tenant, w.Operation})
	h := sha256.Sum256(raw)
	return "credit_" + hex.EncodeToString(h[:])
}
