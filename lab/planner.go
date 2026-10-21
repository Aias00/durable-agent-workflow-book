package lab

import (
	"bytes"
	"errors"
)

// Proposal is untrusted model content. It has no identity, approval, endpoint,
// retry, capability or executable credential fields.
type Proposal struct {
	Version     int    `json:"version"`
	Action      string `json:"action"`
	Account     string `json:"account"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}
type ToolContract struct {
	Name                             string
	Idempotency, AuthoritativeLookup bool
}

func AdmitProposal(raw []byte, contract ToolContract) (Plan, error) {
	if len(raw) > 4096 {
		return Plan{}, errors.New("proposal exceeds admission bound")
	}
	if contract.Name != "credit-service" || contract.Idempotency && !contract.AuthoritativeLookup {
		return Plan{}, errors.New("untrusted or incomplete tool capability contract")
	}
	var proposal Proposal
	if err := strictJSON(bytes.NewReader(raw), &proposal); err != nil {
		return Plan{}, err
	}
	plan := Plan{Version: proposal.Version, Action: proposal.Action, Account: proposal.Account, AmountMinor: proposal.AmountMinor, Currency: proposal.Currency, Idempotent: contract.Idempotency}
	return plan, plan.Validate()
}
