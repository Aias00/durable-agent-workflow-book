# Real PostgreSQL workflow lab

This is one approved `issue_credit` side-effect node. It is not a general workflow engine or an exactly-once payment system. The HTTP tool and workflow tables share one isolated PostgreSQL instance but commit separate transactions.

Use Go 1.24.3 and an empty development PostgreSQL instance. The integration job uses PostgreSQL 17.11, pinned to the observed image digest. For a local Docker installation:

```sh
docker run --rm --name durable-workflow-lab -p 127.0.0.1:25432:5432 \
  -e POSTGRES_USER=book -e POSTGRES_PASSWORD=book-lab-only -e POSTGRES_DB=book \
  postgres:17@sha256:a63c1ea3f6a3a5a1621f97d2aba3b30faecfab4224c1b0000cc85ced57bb1939
```

In another terminal, from the repository root:

```sh
export BOOK_LAB_DSN='postgres://book:book-lab-only@127.0.0.1:25432/book?sslmode=disable'
go test -race -count=1 -json ./... > tests.jsonl
```

Each database test creates a fresh `wf_<16 hexadecimal characters>` schema. Only its creator drops that schema. Worker subprocesses open the existing schema and cannot drop it through `Store.Close`. Never point the lab at a production database. The database primary, fallback hosts, and tool URL/dial destination are restricted to loopback. These development passwords and the fixed HTTP token are not a production authentication system.

Without `BOOK_LAB_DSN`, integration tests explicitly skip: a green local compile is not integration evidence. The Actions collector rejects every skip or failure and retains raw `tests.jsonl`, actual server version, actual image digest, and `runtime.json` with test outcomes and `BOOK_OBSERVATION` values. Artifact name: `workflow-lab-evidence`. The CI command is exactly `go test -race -count=1 -json ./...`.

Run an individual experiment with `-run`, for example:

```sh
go test -race -count=1 -v ./lab -run TestSIGKILLAfterToolReconcilesExistingEffect
```

The harness builds `cmd/worker` with `-race`, starts real OS processes, waits for a printed stage, sends SIGKILL, and lets a real three-second lease expire according to the PostgreSQL clock. A fresh process then reads durable state and authoritative tool receipts. Two-worker contention uses stdin gates to release both claimers concurrently. Other exact boundary tests explicitly label SQL-injected lease/approval expiry, so those are not elapsed-time experiments.

The command flags are `-schema`, `-tool`, `-name`, optional `-start-gate`, optional `-pause after_claim|after_tool`, and optional `-reconcile <workflow-id>`. A newline releases a gate. The reconcile flag first queries an unknown workflow, then runs at most one ready workflow. The harness controls these flags; the CLI does not create or approve tasks.

Recovery rules:

* Every expired running lease becomes `unknown`, with a higher fence.
* A matching authoritative receipt completes that fact without another POST.
* An authoritative 404 may requeue the original key only for the trusted idempotent contract, without cancellation, and with remaining persistent budget.
* Unsupported lookup/idempotency, failed lookup, exhausted unknown budget, or cancellation during lookup leaves the outcome unresolved for manual review.
* Only the trusted tool's explicit 429 `rejected_before_effect` proof permits timed retry. Arbitrary HTTP errors do not prove absence of an effect.
* `workflows.max_attempts` defaults to three. Claim, retry and reconciliation all use that persistent budget. Worker restart cannot reset it.
* Cancellation before the dispatch permission check prevents the call. Cancellation after an effect preserves `succeeded` plus `cancel_requested=true`. There is no automatic compensation and there is an unavoidable check-to-network race without provider cooperation.

The planner accepts only a bounded proposal schema. Tenant, actor, endpoint, approval and idempotency authority come from the application and trusted tool registry. `Store.Create` accepts an already admitted `Plan`; it is not a public model-input endpoint. Approval is bound to the stable plan fingerprint. The old worker refuses persisted plan version 2. Completion and reconciliation both validate receipt key, fingerprint, account, amount and receipt ID.

`events` stores transition kinds and controlled metadata, not raw proposals, tokens, account strings or HTTP bodies. Business data remains in the workflow and tool ledger by design. The privacy experiment covers event kind/detail only, not all possible application telemetry.

The test matrix covers 26 top-level tests, including the two subcases of the non-idempotent SIGKILL test. It does not prove multi-cluster availability, network partition tolerance, model planning quality, real payment-provider semantics, a global workflow deadline, or production identity authentication. Approval expiry is the executable time permission boundary in this lab. Destructive process exit does not simulate a disk or PostgreSQL failure.
