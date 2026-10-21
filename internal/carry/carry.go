// Package carry keeps the decisions recorded on findings, an agent's
// classification and a person's acknowledgement, across runs: past reports
// in, decided findings out (knowledge/decisions/finding-classification.md).
//
// It reads no files. verify hands it every tier's latest report and
// history; a rescore in place hands it the report being rescored.
package carry

import (
	"time"

	"github.com/hbaldwin98/reliab/internal/report"
)

// Decisions holds, per finding id, the newest classification and the newest
// acknowledgement in a set of reports.
type Decisions map[string]decided

// decided is the newest classification and the newest acknowledgement
// recorded for one finding id, each in a copy of the finding that holds it,
// with the run each was read from (tier and run start).
type decided struct {
	classified, acked         *report.Finding
	classifiedFrom, ackedFrom string
}

// Collect gathers, per finding id, the newest classification and
// acknowledgement in past, read in order: on a tie the later record read
// wins. A decision made in one tier holds in another, and through a run
// that skipped the command or ran it on demand.
func Collect(past []*report.Report) Decisions {
	out := Decisions{}
	for _, r := range past {
		from := source(r)
		for i := range r.Findings {
			f := r.Findings[i]
			d := out[f.ID]
			if f.Classification != nil && (d.classified == nil || newer(f.Classification.At, d.classified.Classification.At)) {
				d.classified, d.classifiedFrom = &f, from
			}
			if f.Acknowledged != nil && (d.acked == nil || newer(f.Acknowledged.At, d.acked.Acknowledged.At)) {
				d.acked, d.ackedFrom = &f, from
			}
			out[f.ID] = d
		}
	}
	return out
}

// Apply keeps the newest classification and acknowledgement for each
// finding in rep, matched by its stable id. A carried decision names the
// run it was made in (from), kept through later carries, so a reviewer can
// tell it from one made on this run; one read from rep's own run names
// none. A finding that already holds the decision is left as it is, so
// applying twice changes nothing.
//
// A command-level finding (no test: the command failed as a whole) has one
// id however it failed, so a decision carries only while its fingerprint is
// the same; otherwise it is judged afresh and says why. An environment
// failure says nothing about the next run and never carries.
func (d Decisions) Apply(rep *report.Report) {
	own := source(rep)
	for i := range rep.Findings {
		f := &rep.Findings[i]
		dec := d[f.ID]
		if c := dec.classified; c != nil && c.Kind != report.KindEnvironment && !holds(f.Classification, c.Classification.At, f.Kind == c.Kind) {
			if why := refused(*c, *f, "classified "+c.Kind); why != "" {
				f.Reopened = why
			} else {
				cl := *c.Classification
				if cl.From == "" && dec.classifiedFrom != own {
					cl.From = dec.classifiedFrom
				}
				f.Kind = c.Kind
				f.Classification = &cl
			}
		}
		if a := dec.acked; a != nil && a.Kind != report.KindEnvironment && !holdsAck(f.Acknowledged, a.Acknowledged.At) {
			if why := refused(*a, *f, "acknowledged by "+a.Acknowledged.By); why != "" {
				f.Reopened = why
			} else {
				ack := *a.Acknowledged
				if ack.From == "" && dec.ackedFrom != own {
					ack.From = dec.ackedFrom
				}
				f.Acknowledged = &ack
				f.Status = report.StatusAcknowledged
			}
		}
	}
}

// source names the run a report records, the form From takes.
func source(r *report.Report) string { return r.Run.Tier + " " + r.Run.StartedAt }

// holds reports whether a finding already has the classification made at
// at, of the same kind.
func holds(c *report.Classification, at string, sameKind bool) bool {
	return c != nil && sameKind && c.At == at
}

// holdsAck reports whether a finding already has the acknowledgement made
// at at.
func holdsAck(a *report.Acknowledged, at string) bool { return a != nil && a.At == at }

// newer reports whether timestamp a is after b, as moments, whatever their
// zones; on a tie the later record read wins. A timestamp that does not
// parse loses to one that does; between two that do not, the later record
// read wins.
func newer(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return errA == nil || errB != nil
	}
	return !ta.Before(tb)
}

// refused says why a command-level finding's earlier decision (what) does
// not carry to cur, or "" when it does: always for a finding with a test,
// and for a command-level one while the failure is the same. An earlier
// report without a fingerprint cannot show that, so nothing carries from
// it.
func refused(old, cur report.Finding, what string) string {
	if cur.Test != "" {
		return ""
	}
	if old.Fingerprint == "" {
		return "it was " + what + " in a report that recorded no fingerprint of the failure, so that decision was not carried; judge it afresh"
	}
	if old.Fingerprint != cur.Fingerprint {
		return "the failure changed since it was " + what + "; judge it afresh"
	}
	return ""
}
