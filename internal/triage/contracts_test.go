package triage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/verify"
)

// knowledge/decisions/consumer-contracts.md: a labelled finding from the
// contracts command gets the contract advice, where an addition is a new
// demand on the provider; a labelled snapshot keeps the snapshot advice.
func TestRouteContractDiffs(t *testing.T) {
	root := testrepo.New(t, `commands:
  snapshots:
    run: cp s.json .reliab/out/s.json
    format: findings
    output: .reliab/out/s.json
  contracts:
    run: cp c.json .reliab/out/c.json
    format: findings
    output: .reliab/out/c.json
`)
	for name, body := range map[string]string{
		"s.json": `{"schemaVersion":"0.2.0","findings":[{"test":"snapshots/a.snap","diff":"additive-only","message":"+a"}]}`,
		"c.json": `{"schemaVersion":"0.2.0","findings":[{"test":"pact checkout -> orders","diff":"additive-only","message":"+a"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := verify.Run(context.Background(), verify.Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Route(&buf, root); err != nil {
		t.Fatal(err)
	}
	blocks := map[string]string{}
	for _, b := range strings.Split(buf.String(), "\n\n")[1:] {
		head, _, _ := strings.Cut(b, "\n")
		blocks[head[strings.LastIndex(head, "]")+1:]] = b
	}
	contract, snapshot := blocks["  pact checkout -> orders"], blocks["  snapshots/a.snap"]
	if !strings.Contains(contract, "needs more from the provider") || strings.Contains(contract, "a pinned output changed") {
		t.Errorf("contract routed without the contract advice:\n%s", contract)
	}
	if !strings.Contains(snapshot, "a pinned output changed") || strings.Contains(snapshot, "provider") {
		t.Errorf("snapshot routed with the contract advice:\n%s", snapshot)
	}
}
