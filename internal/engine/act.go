package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/ifuryst/open-browser-use/internal/engine/js"
)

// MaxSteps is the most steps one Act takes.
const MaxSteps = 20

// Ops are the step operations (Wayfarer's vocabulary without secrets, uploads and dialogs):
//
//	click   index          click the control
//	type    index, value   replace a text field's contents
//	select  index, value   choose a <select> option by value or label
//	check   index, value?  set a checkbox, radio or switch to "true" (default) or "false"
//	press   value          press a key (Keys)
//	scroll  value?         "down" (default) or "up"
//	wait    value?         wait value milliseconds (default 1500, at most 15000)
//	back                   go back one page
var Ops = []string{"click", "type", "select", "check", "press", "scroll", "wait", "back"}

// Step is one action on an observed control. Index is the control's index in the page's last observation.
type Step struct {
	Op    string `json:"op"`
	Index int    `json:"index,omitempty"`
	Value string `json:"value,omitempty"`
}

// StepResult is what became of one step: done, failed (Error says why) or skipped after an earlier failure.
type StepResult struct {
	Step   int    `json:"step"`
	Op     string `json:"op"`
	Index  int    `json:"index,omitempty"`
	Label  string `json:"label,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// ActResult is each step's result and the page after them.
type ActResult struct {
	Steps       []StepResult `json:"steps"`
	Observation *Observation `json:"observation"`
}

type key struct {
	key, code string
	keyCode   int
	text      string
}

var keys = map[string]key{
	"Enter":      {"Enter", "Enter", 13, "\r"},
	"Tab":        {"Tab", "Tab", 9, ""},
	"Escape":     {"Escape", "Escape", 27, ""},
	"Backspace":  {"Backspace", "Backspace", 8, ""},
	"Delete":     {"Delete", "Delete", 46, ""},
	"Space":      {" ", "Space", 32, " "},
	"ArrowUp":    {"ArrowUp", "ArrowUp", 38, ""},
	"ArrowDown":  {"ArrowDown", "ArrowDown", 40, ""},
	"ArrowLeft":  {"ArrowLeft", "ArrowLeft", 37, ""},
	"ArrowRight": {"ArrowRight", "ArrowRight", 39, ""},
	"PageDown":   {"PageDown", "PageDown", 34, ""},
	"PageUp":     {"PageUp", "PageUp", 33, ""},
	"Home":       {"Home", "Home", 36, ""},
	"End":        {"End", "End", 35, ""},
}

// Keys are the key names press takes.
var Keys = []string{"Enter", "Tab", "Escape", "Backspace", "Delete", "Space", "ArrowUp", "ArrowDown", "ArrowLeft",
	"ArrowRight", "PageDown", "PageUp", "Home", "End"}

var reasons = map[string]string{"gone": "is gone", "changed": "has changed", "disabled": "is disabled",
	"readonly": "is read-only", "offscreen": "is off screen", "covered": "is covered by something else"}

// CheckSteps validates steps without a page.
func CheckSteps(steps []Step) error {
	if len(steps) < 1 || len(steps) > MaxSteps {
		return inputf("provide 1-%d steps", MaxSteps)
	}
	for i, s := range steps {
		n := i + 1
		if !slices.Contains(Ops, s.Op) {
			return inputf("step %d: op must be one of %v", n, Ops)
		}
		needsIndex := slices.Contains([]string{"click", "type", "select", "check"}, s.Op)
		if needsIndex && s.Index < 1 {
			return inputf("step %d: %s needs an index", n, s.Op)
		}
		if !needsIndex && s.Index != 0 {
			return inputf("step %d: %s takes no index", n, s.Op)
		}
		switch s.Op {
		case "select":
			if s.Value == "" {
				return inputf("step %d: select needs a value", n)
			}
		case "check":
			if s.Value != "" && s.Value != "true" && s.Value != "false" {
				return inputf("step %d: check value is true or false", n)
			}
		case "press":
			if _, ok := keys[s.Value]; !ok {
				return inputf("step %d: keys are %v", n, Keys)
			}
		case "scroll":
			if s.Value != "" && s.Value != "down" && s.Value != "up" {
				return inputf("step %d: scroll value is down or up", n)
			}
		case "wait":
			if s.Value != "" {
				if ms, err := strconv.Atoi(s.Value); err != nil || ms < 0 {
					return inputf("step %d: wait value is milliseconds", n)
				}
			}
		}
	}
	return nil
}

// Act runs steps against the page's last observation, stopping at the first failure, then observes the page again.
// The first step re-checks its control strictly (the same identity, name and surroundings as observed); later steps
// only need the control to still be there, since earlier steps are expected to change the page.
func (p *Page) Act(ctx context.Context, steps []Step) (*ActResult, error) {
	if err := CheckSteps(steps); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	planned := p.last
	elements := make([]*Element, len(steps))
	for i, s := range steps {
		if s.Index == 0 {
			continue
		}
		if planned == nil {
			return nil, inputf("observe the page first")
		}
		e := element(planned, s.Index)
		if e == nil {
			return nil, inputf("no control [%d] on the page's last observation; observe it again", s.Index)
		}
		elements[i] = e
	}
	res := &ActResult{}
	failed := false
	for i, s := range steps {
		r := StepResult{Step: i + 1, Op: s.Op, Index: s.Index}
		e := elements[i]
		if e != nil {
			r.Label = e.Label
		}
		if failed {
			r.Status = "skipped"
			res.Steps = append(res.Steps, r)
			continue
		}
		err := p.step(ctx, s, e, i == 0, planned)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			r.Status, r.Error, failed = "failed", stepError(err), true
		} else {
			r.Status = "done"
			p.settle(ctx, e, planned)
			if slices.Contains([]string{"click", "press", "back", "check"}, s.Op) {
				if err := p.loaded(ctx, 8*time.Second); err != nil {
					return nil, err
				}
			}
		}
		res.Steps = append(res.Steps, r)
	}
	obs, err := p.observe(ctx)
	if err != nil {
		return nil, err
	}
	res.Observation = obs
	return res, nil
}

func stepError(err error) string {
	var ie *InputError
	var se *StaleError
	var de *DetachedError
	if errors.As(err, &ie) || errors.As(err, &se) || errors.As(err, &de) {
		return err.Error()
	}
	return "the browser refused: " + err.Error()
}

func element(obs *Observation, index int) *Element {
	if obs == nil || index < 1 || index > len(obs.Elements) {
		return nil
	}
	e := obs.Elements[index-1]
	return &e
}

func (p *Page) step(ctx context.Context, s Step, e *Element, strict bool, planned *Observation) error {
	switch s.Op {
	case "click":
		pt, err := p.locate(ctx, e, "click", strict, planned)
		if err != nil {
			return err
		}
		return p.click(ctx, pt)
	case "type":
		if !slices.Contains(e.Ops, "type") {
			return inputf("[%d] is not a text field", e.Index)
		}
		pt, err := p.locate(ctx, e, "type", strict, planned)
		if err != nil {
			return err
		}
		if err := p.click(ctx, pt); err != nil {
			return err
		}
		return p.replaceText(ctx, s.Value)
	case "select":
		if _, err := p.locate(ctx, e, "select", strict, planned); err != nil {
			return err
		}
		var ok bool
		args, _ := json.Marshal(s.Value)
		if _, err := p.eval(ctx, planned.ctxID, fmt.Sprintf("(%s)(%d, %s)", js.Select, e.Node, args), false, &ok); err != nil {
			return err
		}
		if !ok {
			return inputf("[%d] has no option %s", e.Index, quote(s.Value))
		}
		return nil
	case "check":
		if !slices.Contains([]string{"checkbox", "radio", "switch", "menuitemcheckbox", "menuitemradio"}, e.Role) {
			return inputf("[%d] is not a checkbox, radio button or switch", e.Index)
		}
		want := strconv.FormatBool(s.Value != "false")
		if e.Checked == want {
			return nil
		}
		if want == "false" && e.Role == "radio" {
			return inputf("[%d] is a radio button; choose another option instead", e.Index)
		}
		pt, err := p.locate(ctx, e, "click", strict, planned)
		if err != nil {
			return err
		}
		return p.click(ctx, pt)
	case "press":
		return p.press(ctx, s.Value)
	case "scroll":
		dy := 600.0
		if s.Value == "up" {
			dy = -600
		}
		vp := size{1280, 720}
		if planned != nil && planned.viewport.Width > 0 {
			vp = planned.viewport
		}
		return p.input(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": vp.Width / 2,
			"y": vp.Height / 2, "deltaX": 0, "deltaY": dy})
	case "wait":
		ms, _ := strconv.Atoi(s.Value)
		if ms == 0 {
			ms = 1500
		}
		return sleep(ctx, time.Duration(min(ms, 15_000))*time.Millisecond)
	case "back":
		return p.back(ctx)
	}
	return inputf("unknown op %q", s.Op)
}

type point struct{ X, Y float64 }

// locate re-checks an observed control in the page and returns the point to act at. The control must still be in
// the same document the observation was taken in.
func (p *Page) locate(ctx context.Context, e *Element, op string, strict bool, planned *Observation) (point, error) {
	stale := func(reason string) error {
		r, ok := reasons[reason]
		if !ok {
			r = "is not available"
		}
		return &StaleError{fmt.Sprintf("[%d] %s %s; observe the page again", e.Index, e.Label, r)}
	}
	k, id, err := p.context(ctx)
	var de *DetachedError
	if errors.As(err, &de) {
		return point{}, err
	}
	if err != nil || k != planned.world {
		return point{}, stale("gone")
	}
	expected := "null"
	if strict {
		if g, ok := planned.guards[e.Node]; ok {
			expected = string(g)
		}
	}
	var t struct {
		X, Y  float64
		Error string
	}
	ok, err := p.eval(ctx, id, fmt.Sprintf("(%s)(%d, %s, %s)", js.Target, e.Node, expected, quote(op)), false, &t)
	if err != nil {
		var se *StaleError
		if errors.As(err, &se) {
			return point{}, stale("gone")
		}
		return point{}, err
	}
	if !ok || t.Error != "" {
		return point{}, stale(t.Error)
	}
	return point{t.X, t.Y}, nil
}

func (p *Page) input(ctx context.Context, method string, params any) error {
	_, err := p.cdp.Call(ctx, method, params)
	return err
}

func (p *Page) click(ctx context.Context, pt point) error {
	if err := p.input(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": pt.X, "y": pt.Y}); err != nil {
		return err
	}
	for _, t := range []string{"mousePressed", "mouseReleased"} {
		if err := p.input(ctx, "Input.dispatchMouseEvent", map[string]any{"type": t, "x": pt.X, "y": pt.Y,
			"button": "left", "clickCount": 1}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Page) press(ctx context.Context, name string) error {
	k, ok := keys[name]
	if !ok {
		return inputf("keys are %v", Keys)
	}
	down := map[string]any{"type": "rawKeyDown", "key": k.key, "code": k.code, "windowsVirtualKeyCode": k.keyCode,
		"nativeVirtualKeyCode": k.keyCode}
	if k.text != "" {
		down["type"], down["text"] = "keyDown", k.text
	}
	if err := p.input(ctx, "Input.dispatchKeyEvent", down); err != nil {
		return err
	}
	return p.input(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": k.key, "code": k.code,
		"windowsVirtualKeyCode": k.keyCode, "nativeVirtualKeyCode": k.keyCode})
}

// replaceText replaces a focused field's contents: select all, then insert (or delete).
func (p *Page) replaceText(ctx context.Context, text string) error {
	if err := p.input(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "rawKeyDown", "key": "a", "code": "KeyA",
		"windowsVirtualKeyCode": 65, "modifiers": 2, "commands": []string{"selectAll"}}); err != nil {
		return err
	}
	if err := p.input(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "a", "code": "KeyA",
		"windowsVirtualKeyCode": 65, "modifiers": 2}); err != nil {
		return err
	}
	if text == "" {
		return p.press(ctx, "Backspace")
	}
	return p.input(ctx, "Input.insertText", map[string]any{"text": text})
}

// settle waits briefly for the page to react to a step.
func (p *Page) settle(ctx context.Context, e *Element, planned *Observation) {
	node := 0
	if e != nil {
		node = e.Node
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, id, err := p.context(c)
	if err != nil {
		return
	}
	p.eval(c, id, fmt.Sprintf("(%s)(%d)", js.Settle, node), true, nil)
}

func (p *Page) back(ctx context.Context) error {
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := p.call(ctx, "Page.getNavigationHistory", map[string]any{}, &h); err != nil {
		return err
	}
	if h.CurrentIndex < 1 || h.CurrentIndex > len(h.Entries)-1 {
		return inputf("there is no earlier page")
	}
	return p.input(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": h.Entries[h.CurrentIndex-1].ID})
}

// loaded waits for the document to finish loading (at most maxWait; slow pages are read as they are).
func (p *Page) loaded(ctx context.Context, maxWait time.Duration) error {
	// A click or Enter may start a navigation a moment later; give it the chance to begin.
	if err := sleep(ctx, 150*time.Millisecond); err != nil {
		return err
	}
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		var state string
		_, id, err := p.context(c)
		if err == nil {
			var ok bool
			ok, err = p.eval(c, id, "document.readyState", false, &state)
			if !ok {
				state = ""
			}
		}
		cancel()
		if err == nil && state == "complete" {
			break
		}
		if err := sleep(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	return sleep(ctx, 250*time.Millisecond)
}
