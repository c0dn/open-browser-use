package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ifuryst/open-browser-use/internal/engine"
)

// Page is what the task loop needs from a tab: the engine's observe and act.
type Page interface {
	Observe(ctx context.Context) (*engine.Observation, error)
	Act(ctx context.Context, steps []engine.Step) (*engine.ActResult, error)
}

// Status is how a task ended.
type Status string

const (
	StatusDone          Status = "done"
	StatusBlocked       Status = "blocked"
	StatusNeedsOwner    Status = "needs_owner"
	StatusNeedsInfo     Status = "needs_info"
	StatusNeedsApproval Status = "needs_approval"
	StatusLimit         Status = "limit"
)

// Options bound a task.
type Options struct {
	MaxActions int  // actions taken (default 30)
	AllowSpend bool // let clicks that may spend money run
}

// Result is a finished task.
type Result struct {
	Status    Status                      `json:"status"`
	Note      string                      `json:"note"`
	Steps     []HistoryItem               `json:"steps"`
	Decisions int                         `json:"decisions"`
	Seconds   float64                     `json:"seconds"`
	Tokens    struct{ Input, Output int } `json:"tokens"`
	Cost      float64                     `json:"cost"`
	Page      *engine.Observation         `json:"-"`
}

// Run works towards goal on the page, one Jev decision per step, until Jev reports DONE, BLOCKED or NEEDS_OWNER, the
// page needs a password or code, three actions change nothing, or the action limit is reached.
func (c Config) Run(ctx context.Context, p Page, goal string, opt Options) (*Result, error) {
	if opt.MaxActions <= 0 {
		opt.MaxActions = 30
	}
	started := time.Now()
	res := &Result{Steps: []HistoryItem{}}
	end := func(status Status, note string) (*Result, error) {
		res.Status, res.Note = status, note
		res.Seconds = float64(time.Since(started).Milliseconds()/100) / 10
		return res, nil
	}
	page, err := p.Observe(ctx)
	if err != nil {
		return nil, err
	}
	// A single-page app is often still empty when the document reports loaded.
	for waited := 0; len(page.Elements) == 0 && waited < 8000; waited += 500 {
		if err := sleep(ctx, 500*time.Millisecond); err != nil {
			return nil, err
		}
		if page, err = p.Observe(ctx); err != nil {
			return nil, err
		}
	}
	res.Page = page
	if !opt.AllowSpend && money.MatchString(goal) {
		return end(StatusNeedsApproval, "The goal may spend money; run it again with allow_spend once the owner agrees.")
	}
	rechecked := false
	// <= so the decision after the last allowed action can still report DONE; only an action past the limit stops it.
	for len(res.Steps) <= opt.MaxActions && res.Decisions <= 2*opt.MaxActions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if note := ownerStep(page); note != "" {
			return end(StatusNeedsOwner, note)
		}
		res.Decisions++
		d, err := c.decide(ctx, page, goal, res.Steps)
		if err != nil {
			return nil, err
		}
		res.Tokens.Input += d.Usage.InputTokens
		res.Tokens.Output += d.Usage.OutputTokens
		res.Cost += d.Usage.Cost

		switch d.Operation {
		case Done:
			// Done straight after a click that changed nothing usually means the page hadn't caught up yet.
			if n := len(res.Steps); !rechecked && n > 0 && res.Steps[n-1].Operation == Click && !deref(res.Steps[n-1].PageChanged) {
				rechecked = true
				sleep(ctx, 1500*time.Millisecond)
				again, err := p.Observe(ctx)
				if err != nil {
					return nil, err
				}
				if !bytes.Equal(again.Marker, page.Marker) {
					page, res.Page = again, again
					continue
				}
			}
			return end(StatusDone, "The policy reports every requirement visible; check the page.")
		case Blocked:
			return end(StatusBlocked, "No offered action makes progress from this page.")
		case NeedsOwner:
			return end(StatusNeedsOwner, "The page needs the owner (password, code, CAPTCHA, approval or payment consent).")
		}
		rechecked = false
		if len(res.Steps) >= opt.MaxActions {
			break
		}

		steps, item, stop, err := c.plan(ctx, d, page, goal, res.Steps, opt)
		if err != nil {
			return nil, err
		}
		if stop != nil {
			return end(stop.status, stop.note)
		}
		before := page
		if len(steps) > 0 {
			out, err := p.Act(ctx, steps)
			if err != nil {
				var ie *engine.InputError
				if !errors.As(err, &ie) {
					return nil, err
				}
				// The plan named a control the observation no longer has: look again and decide again.
				if page, err = p.Observe(ctx); err != nil {
					return nil, err
				}
				res.Page = page
				continue
			}
			page, res.Page = out.Observation, out.Observation
			for _, s := range out.Steps {
				if s.Status == "failed" {
					item.Error = s.Error
					break
				}
			}
		} else if d.Operation == Wait {
			sleep(ctx, 600*time.Millisecond)
			if page, err = p.Observe(ctx); err != nil {
				return nil, err
			}
			res.Page = page
		}
		changed := !bytes.Equal(before.Marker, page.Marker)
		item.PageChanged = &changed
		res.Steps = append(res.Steps, item)

		recent := tail(res.Steps, 3)
		if len(recent) == 3 && stuck(recent) {
			return end(StatusBlocked, "Three actions in a row changed nothing.")
		}
	}
	return end(StatusLimit, fmt.Sprintf("Stopped after %d actions; call again to continue.", len(res.Steps)))
}

type stopReason struct {
	status Status
	note   string
}

// plan turns a decision into engine steps, writing field values with the text model.
func (c Config) plan(ctx context.Context, d *Decision, page *engine.Observation, goal string, history []HistoryItem, opt Options) ([]engine.Step, HistoryItem, *stopReason, error) {
	label := func(e *engine.Element) string { return fmt.Sprintf("[%d] %s", e.Index, e.Label) }
	item := HistoryItem{Operation: d.Operation, Action: string(d.Operation)}
	spend := func(e *engine.Element) *stopReason {
		if opt.AllowSpend || !(money.MatchString(page.URL) || money.MatchString(page.Title) || money.MatchString(e.Label)) {
			return nil
		}
		return &stopReason{StatusNeedsApproval, fmt.Sprintf("Clicking %s may spend money; run again with allow_spend once the owner agrees.", label(e))}
	}
	switch d.Operation {
	case Click:
		if s := spend(d.Element); s != nil {
			return nil, item, s, nil
		}
		item.Action = "CLICK " + label(d.Element)
		return []engine.Step{{Op: "click", Index: d.Element.Index}}, item, nil, nil
	case TypeText:
		text, err := c.fieldText(ctx, goal, *d.Element, page, history)
		if err != nil {
			return nil, item, nil, err
		}
		if text == nil {
			return nil, item, &stopReason{StatusNeedsInfo, "The goal doesn't say what to enter in " + label(d.Element) + "."}, nil
		}
		item.Action, item.Text = "TYPE_TEXT "+label(d.Element), text
		return []engine.Step{{Op: "type", Index: d.Element.Index, Value: *text}}, item, nil, nil
	case Select:
		item.Action = "SELECT " + label(d.Element)
		return []engine.Step{{Op: "select", Index: d.Element.Index, Value: d.Value}}, item, nil, nil
	case ScrollDown, ScrollUp:
		dir := "down"
		if d.Operation == ScrollUp {
			dir = "up"
		}
		return []engine.Step{{Op: "scroll", Value: dir}}, item, nil, nil
	case Back:
		return []engine.Step{{Op: "back"}}, item, nil, nil
	case Wait:
		return nil, item, nil, nil
	case FillForm:
		return c.fillForm(ctx, d, page, goal, history, label, spend)
	}
	return nil, item, &stopReason{StatusBlocked, "Unknown operation " + string(d.Operation)}, nil
}

// fillForm writes every chosen field's value at once (text model calls run in parallel), then one act sets them all
// and presses the submit button. A field the goal gives no value for stops the task.
func (c Config) fillForm(ctx context.Context, d *Decision, page *engine.Observation, goal string, history []HistoryItem,
	label func(*engine.Element) string, spend func(*engine.Element) *stopReason) ([]engine.Step, HistoryItem, *stopReason, error) {
	item := HistoryItem{Operation: FillForm}
	if len(d.Fields) == 0 && d.Submit == nil {
		item.Action = "FILL_FORM (nothing to set)"
		return nil, item, nil, nil
	}
	type answer struct {
		text *string
		err  error
	}
	answers := make([]answer, len(d.Fields))
	var wg sync.WaitGroup
	for i, f := range d.Fields {
		if !has(f.Ops, "type") && !has(f.Ops, "select") {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			t, err := c.fieldText(ctx, goal, f, page, history)
			answers[i] = answer{t, err}
		}()
	}
	wg.Wait()
	var steps []engine.Step
	var parts []string
	for i, f := range d.Fields {
		e := f
		switch {
		case has(f.Ops, "type") || has(f.Ops, "select"):
			a := answers[i]
			if a.err != nil {
				return nil, item, nil, a.err
			}
			if a.text == nil {
				return nil, item, &stopReason{StatusNeedsInfo, "The goal doesn't say what to enter in " + label(&e) + "."}, nil
			}
			if has(f.Ops, "type") {
				steps = append(steps, engine.Step{Op: "type", Index: f.Index, Value: *a.text})
				parts = append(parts, fmt.Sprintf("%s = %q", label(&e), *a.text))
				continue
			}
			value := ""
			for _, o := range f.Options {
				if o.Value == *a.text || strings.EqualFold(o.Label, *a.text) {
					value = o.Value
				}
			}
			if value == "" {
				return nil, item, &stopReason{StatusNeedsInfo, fmt.Sprintf("%s has no option %q.", label(&e), *a.text)}, nil
			}
			steps = append(steps, engine.Step{Op: "select", Index: f.Index, Value: value})
			parts = append(parts, fmt.Sprintf("%s = %q", label(&e), *a.text))
		default:
			// A checkbox, radio or switch: Jev says it must change, so set it to the other state (never toggle blindly).
			want := "true"
			if f.Checked == "true" {
				want = "false"
			}
			steps = append(steps, engine.Step{Op: "check", Index: f.Index, Value: want})
			parts = append(parts, fmt.Sprintf("set %s %s", label(&e), want))
		}
	}
	if d.Submit != nil {
		if s := spend(d.Submit); s != nil {
			return nil, item, s, nil
		}
		steps = append(steps, engine.Step{Op: "click", Index: d.Submit.Index})
		parts = append(parts, "submit "+label(d.Submit))
	}
	item.Action = "FILL_FORM " + strings.Join(parts, ", ")
	return steps, item, nil, nil
}

// stuck: three actions that changed nothing (waiting and typing don't count; typing changes no marker).
func stuck(recent []HistoryItem) bool {
	for _, s := range recent {
		if deref(s.PageChanged) || s.Operation == Wait || s.Operation == TypeText {
			return false
		}
	}
	return true
}

func deref(b *bool) bool { return b != nil && *b }

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
