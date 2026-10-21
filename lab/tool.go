package lab

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const DevelopmentToolToken = "book-tool-only"

type Receipt struct {
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
	ID          string `json:"id"`
	Account     string `json:"account"`
	AmountMinor int64  `json:"amount_minor"`
}

func (r Receipt) Validate(w Workflow) error {
	if r.Key != w.EffectKey() || r.Fingerprint != w.Fingerprint || r.ID == "" || r.Account != w.Plan.Account || r.AmountMinor != w.Plan.AmountMinor {
		return errors.New("tool receipt does not match approved intent")
	}
	return nil
}

type ToolRequest struct {
	Key  string `json:"key"`
	Plan Plan   `json:"plan"`
}

// ToolService is a separate HTTP boundary with its own PostgreSQL transaction.
// It shares the lab instance, not the workflow transaction. It is not a payment provider.
type ToolService struct {
	DB              *sql.DB
	Idempotent      bool
	DropResponses   atomic.Bool
	RejectRemaining atomic.Int32
	Calls           atomic.Int64
	Committed       chan struct{}
	once            sync.Once
}

func NewTool(db *sql.DB, idempotent bool) *ToolService {
	return &ToolService{DB: db, Idempotent: idempotent, Committed: make(chan struct{})}
}
func strictJSON(r io.Reader, out any) error {
	d := json.NewDecoder(io.LimitReader(r, 8193))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("only one bounded JSON object is accepted")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (t *ToolService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+DevelopmentToolToken {
		http.Error(w, "denied", 401)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/effects/") {
		if !t.Idempotent {
			http.Error(w, "query contract unavailable", 501)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/effects/")
		var receipt Receipt
		receipt.Key = key
		err := t.DB.QueryRowContext(r.Context(), "SELECT fingerprint,receipt,account,amount_minor FROM tool_effects WHERE effect_key=$1", key).Scan(&receipt.Fingerprint, &receipt.ID, &receipt.Account, &receipt.AmountMinor)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ledger unavailable", 503)
			return
		}
		writeJSON(w, 200, receipt)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/effects" {
		http.NotFound(w, r)
		return
	}
	t.Calls.Add(1)
	var request ToolRequest
	if err := strictJSON(r.Body, &request); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if err := request.Plan.Validate(); err != nil || request.Key == "" || len(request.Key) > 160 {
		http.Error(w, "invalid intent", 400)
		return
	}
	for {
		n := t.RejectRemaining.Load()
		if n <= 0 {
			break
		}
		if t.RejectRemaining.CompareAndSwap(n, n-1) {
			writeJSON(w, 429, map[string]bool{"rejected_before_effect": true})
			return
		}
	}
	ledgerKey := request.Key
	if !t.Idempotent {
		ledgerKey += "_" + ID()
	}
	tx, err := t.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "ledger unavailable", 503)
		return
	}
	defer tx.Rollback()
	fp := request.Plan.Fingerprint()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO tool_effects(effect_key,fingerprint,account,amount_minor,receipt) VALUES($1,$2,$3,$4,$5) ON CONFLICT(effect_key) DO NOTHING`, ledgerKey, fp, request.Plan.Account, request.Plan.AmountMinor, ID())
	if err != nil {
		http.Error(w, "ledger unavailable", 503)
		return
	}
	receipt := Receipt{Key: request.Key}
	err = tx.QueryRowContext(r.Context(), "SELECT fingerprint,receipt,account,amount_minor FROM tool_effects WHERE effect_key=$1", ledgerKey).Scan(&receipt.Fingerprint, &receipt.ID, &receipt.Account, &receipt.AmountMinor)
	if err != nil {
		http.Error(w, "ledger unavailable", 503)
		return
	}
	if receipt.Fingerprint != fp {
		http.Error(w, "key fingerprint conflict", 409)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "commit result unknown", 503)
		return
	}
	t.once.Do(func() { close(t.Committed) })
	if t.DropResponses.Load() {
		h, ok := w.(http.Hijacker)
		if !ok {
			panic("lab fault requires HTTP/1 connection hijacking")
		}
		conn, _, e := h.Hijack()
		if e == nil {
			_ = conn.Close()
		}
		return
	}
	writeJSON(w, 200, receipt)
}

type ToolClient struct {
	Base string
	HTTP *http.Client
}

var ErrRejected = errors.New("trusted tool rejected before effect")

func NewToolClient(base string) (*ToolClient, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("expected a loopback HTTP tool URL")
	}
	if u.Hostname() != "localhost" {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("lab tool must use loopback")
		}
	}
	transport := &http.Transport{DisableKeepAlives: true, ResponseHeaderTimeout: time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if host == "localhost" {
				host = "127.0.0.1"
			}
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return nil, errors.New("non-loopback tool dial rejected")
			}
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(host, port))
		}}
	return &ToolClient{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("tool redirects disabled") }}}, nil
}
func (c *ToolClient) Close() { c.HTTP.CloseIdleConnections() }
func (c *ToolClient) Call(ctx context.Context, w Workflow) (Receipt, error) {
	raw, _ := json.Marshal(ToolRequest{Key: w.EffectKey(), Plan: w.Plan})
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/effects", bytes.NewReader(raw))
	if err != nil {
		return Receipt{}, err
	}
	req.Header.Set("Authorization", "Bearer "+DevelopmentToolToken)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return Receipt{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == 429 {
		var rejected struct {
			Rejected bool `json:"rejected_before_effect"`
		}
		if strictJSON(response.Body, &rejected) == nil && rejected.Rejected {
			return Receipt{}, ErrRejected
		}
	}
	if response.StatusCode != 200 {
		return Receipt{}, fmt.Errorf("tool HTTP %d has no accepted outcome proof", response.StatusCode)
	}
	var receipt Receipt
	if err = strictJSON(response.Body, &receipt); err != nil {
		return receipt, err
	}
	return receipt, receipt.Validate(w)
}
func (c *ToolClient) Query(ctx context.Context, w Workflow) (*Receipt, error) {
	if !w.Plan.Idempotent {
		return nil, ErrManual
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.Base+"/effects/"+w.EffectKey(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+DevelopmentToolToken)
	response, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		return nil, nil
	}
	if response.StatusCode != 200 {
		return nil, errors.New("no authoritative tool query result")
	}
	var receipt Receipt
	if err = strictJSON(response.Body, &receipt); err != nil {
		return nil, err
	}
	return &receipt, receipt.Validate(w)
}
