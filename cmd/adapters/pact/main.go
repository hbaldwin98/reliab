// Command reliab-adapter-pact converts a run of Pact consumer tests into
// reliab's findings format, labelling each contract that differs from its
// committed one additive-only or values-changed
// (knowledge/decisions/consumer-contracts.md).
//
//	rm -rf .reliab/out/pacts; PACT_DIR=.reliab/out/pacts npm test -- --reporter=junit ...;
//	reliab-adapter-pact -exit $? -junit .reliab/out/contracts.xml .reliab/out/pacts pacts .reliab/out/contracts.json
//
// The tests write their pacts to the pending directory (the Pact library's
// dir or PactDir option, read from an environment variable the test setup
// sets), never to the accepted directory, which the profile protects. Clear
// the pending directory first: Pact merges into a pact file already there,
// so a stale interaction would stay. Files are paired by consumer and
// provider name, not file name. Pact's metadata and the order of
// interactions do not count as a change.
//
// A pending contract with no accepted one is new. An accepted contract no
// test wrote is reported only after a clean, whole run: a failing test may
// have stopped Pact writing it, and a run narrowed by --scope
// (RELIAB_SCOPE) writes only some. With -junit, failing tests become
// findings too, so a mismatch the mock server saw is not lost.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hbaldwin98/reliab/pkg/findings"
	"github.com/hbaldwin98/reliab/pkg/snapdiff"
	"github.com/hbaldwin98/reliab/pkg/testresults"
)

func main() {
	exit := flag.Int("exit", -1, "the test run's exit code")
	junit := flag.String("junit", "", "the test run's JUnit report, to keep failing tests")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: reliab-adapter-pact -exit <code> [-junit <junit.xml>] <pending-dir> <accepted-dir> <findings.json>")
	}
	flag.Parse()
	if flag.NArg() != 3 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*exit, *junit, flag.Arg(0), flag.Arg(1), flag.Arg(2), os.Getenv("RELIAB_SCOPE") != ""); err != nil {
		fmt.Fprintln(os.Stderr, "reliab-adapter-pact:", err)
		os.Exit(1)
	}
}

// run writes the findings for one run; scoped is true when verify narrowed
// it to some paths, so a missing contract proves nothing.
func run(exit int, junit, pending, accepted, out string, scoped bool) error {
	cur, err := pacts(pending)
	if err != nil {
		return err
	}
	acc, err := pacts(accepted)
	if err != nil {
		return err
	}
	var fails []findings.Finding
	if junit != "" {
		raw, err := os.ReadFile(junit)
		if err != nil {
			return fmt.Errorf("the run exited %d without a JUnit report: %w", exit, err)
		}
		if fails, err = testresults.ParseJUnit(raw); err != nil {
			return fmt.Errorf("%s: %w", junit, err)
		}
	}
	whole := exit == 0 && len(fails) == 0 && !scoped
	if whole && len(cur) == 0 && len(acc) > 0 {
		return fmt.Errorf("the run wrote no contract to %s but %s holds %d: point the Pact library's pact directory at %s", pending, accepted, len(acc), pending)
	}
	var fs []findings.Finding
	for pair, p := range cur {
		a, ok := acc[pair]
		head, accPath, accBody := "contract changed", a.path, a.body
		if !ok {
			head, accPath, accBody = "new contract, none accepted yet", filepath.Join(accepted, filepath.Base(p.path)), ""
		}
		if f, changed := compare(pair, head, accPath, accBody, p.body, "written to "+filepath.ToSlash(p.path)); changed {
			fs = append(fs, f)
		}
	}
	if whole {
		for pair, a := range acc {
			if _, ok := cur[pair]; !ok {
				f, _ := compare(pair, "no test wrote this contract", a.path, a.body, "", "the service stopped calling the provider, or its tests stopped covering the calls")
				fs = append(fs, f)
			}
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].Test < fs[j].Test })
	fs = append(fs, fails...)
	if len(fs) == 0 && exit != 0 {
		return fmt.Errorf("the run exited %d with no failing test or changed contract; see its log", exit)
	}
	return findings.Write(out, fs)
}

// pact is one contract file: where it is and its canonical rendering.
type pact struct{ path, body string }

// pacts reads every .json file directly in dir as a pact, keyed by
// "consumer -> provider". A missing directory holds none.
func pacts(dir string) (map[string]pact, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]pact{}
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		pair, body, err := canonical(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if prev, ok := out[pair]; ok {
			return nil, fmt.Errorf("%s and %s are both the contract %s", prev.path, path, pair)
		}
		out[pair] = pact{path, body}
	}
	return out, nil
}

// canonical renders a pact so that only a change to the contract shows:
// metadata (library and specification versions) and each interaction's key
// (a hash of the rest, in V4) dropped, interactions and messages sorted by
// description and provider state, object keys sorted, one field per line.
func canonical(raw []byte) (pair, body string, err error) {
	d := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))))
	d.UseNumber()
	var doc map[string]any
	if err := d.Decode(&doc); err != nil {
		return "", "", fmt.Errorf("not a pact: %w", err)
	}
	consumer, provider := name(doc["consumer"]), name(doc["provider"])
	if consumer == "" || provider == "" {
		return "", "", errors.New("not a pact: no consumer or provider name")
	}
	delete(doc, "metadata")
	for _, k := range []string{"interactions", "messages"} {
		list, ok := doc[k].([]any)
		if !ok {
			continue
		}
		keys := make([]string, len(list))
		for i, it := range list {
			if m, ok := it.(map[string]any); ok {
				delete(m, "key")
				keys[i] = sortKey(m)
			}
		}
		sort.Stable(byKey{list, keys})
	}
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(doc); err != nil {
		return "", "", err
	}
	return consumer + " -> " + provider, buf.String(), nil
}

func name(v any) string {
	m, _ := v.(map[string]any)
	s, _ := m["name"].(string)
	return s
}

// sortKey orders an interaction by its description, then its provider
// state (providerState before V3, providerStates from V3).
func sortKey(m map[string]any) string {
	desc, _ := m["description"].(string)
	states, _ := json.Marshal([]any{m["providerState"], m["providerStates"]})
	return desc + "\x00" + string(states)
}

type byKey struct {
	list []any
	keys []string
}

func (b byKey) Len() int           { return len(b.list) }
func (b byKey) Less(i, j int) bool { return b.keys[i] < b.keys[j] }
func (b byKey) Swap(i, j int) {
	b.list[i], b.list[j] = b.list[j], b.list[i]
	b.keys[i], b.keys[j] = b.keys[j], b.keys[i]
}

// compare labels a contract's change; changed is false when the canonical
// renderings are equal.
func compare(pair, head, accPath, acc, cur, note string) (findings.Finding, bool) {
	r := snapdiff.Compare(acc, cur)
	if r.Label == "" {
		return findings.Finding{}, false
	}
	return findings.Finding{
		Test:     "pact " + pair,
		Location: filepath.ToSlash(accPath),
		Message:  fmt.Sprintf("%s: %s (+%d -%d); %s\n%s", head, snapdiff.Describe(r.Label), r.Added, r.Removed, note, r.Diff),
		Diff:     r.Label,
	}, true
}
