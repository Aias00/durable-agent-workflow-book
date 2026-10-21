// plancheck admits one untrusted model proposal. It neither approves nor executes it.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/Aias00/durable-agent-workflow-book/lab"
	"io"
	"os"
)

func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4097))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read bounded proposal")
		os.Exit(1)
	}
	p, err := lab.AdmitProposal(raw, lab.ToolContract{Name: "credit-service", Idempotency: true, AuthoritativeLookup: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "proposal rejected")
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"admitted": true, "fingerprint": p.Fingerprint(), "plan": p, "next_required_stage": "persist_then_wait_for_approval"})
}
