// Package engine observes a tab as numbered controls and acts on them with real input: the fast observe/act loop from
// Wayfarer's engine (itself from Maya and browser-use/jev-ultrafast), run over Open Browser Use's chrome.debugger
// relay instead of a raw CDP socket.
//
// Scripts run only in an isolated world (no Runtime.enable, nothing the page can see). Every action re-checks its
// control (identity, visibility, occlusion) before input, and model output never becomes a selector or code: it can
// only name an index from the last observation.
//
// Limits compared with Wayfarer: one document per tab (same-origin iframes are read, cross-origin iframes are not),
// no event stream (so no dialog or popup tracking), and no secrets, uploads or downloads.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ifuryst/open-browser-use/internal/engine/js"
)

// CDP sends one Chrome DevTools Protocol command to the tab and returns its result.
type CDP interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// worldName is the isolated world the engine's scripts run in.
const worldName = "open-browser-use"

// InputError is a step the caller got wrong; StaleError means the page changed and should be observed again.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

type StaleError struct{ Msg string }

func (e *StaleError) Error() string { return e.Msg }

// DetachedError means the tab's debugger connection is gone; retrying will not help until it is reattached.
type DetachedError struct{ Msg string }

func (e *DetachedError) Error() string { return e.Msg }

// stale turns a failed CDP call into a StaleError (the document is being replaced), unless the debugger is gone.
func stale(err error, msg string) error {
	var de *DetachedError
	if errors.As(err, &de) {
		return err
	}
	return &StaleError{msg}
}

func inputf(format string, a ...any) error { return &InputError{fmt.Sprintf(format, a...)} }

// Page is one tab's engine state: its isolated world and last observation. Safe for one caller at a time; the
// mutex serialises concurrent tool calls on the same tab.
type Page struct {
	cdp CDP

	mu    sync.Mutex
	world worldKey
	ctxID int64
	last  *Observation
}

type worldKey struct{ frame, loader string }

// NewPage wraps a tab's CDP channel.
func NewPage(c CDP) *Page { return &Page{cdp: c} }

func (p *Page) call(ctx context.Context, method string, params any, out any) error {
	raw, err := p.cdp.Call(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// root reads the top document's frame and loader ids.
func (p *Page) root(ctx context.Context) (worldKey, error) {
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID       string `json:"id"`
				LoaderID string `json:"loaderId"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := p.call(ctx, "Page.getFrameTree", map[string]any{}, &tree); err != nil {
		return worldKey{}, stale(err, "the page is navigating")
	}
	f := tree.FrameTree.Frame
	if f.ID == "" {
		return worldKey{}, &StaleError{"the page is navigating"}
	}
	return worldKey{f.ID, f.LoaderID}, nil
}

// context returns the isolated world for the current top document, creating it once per document.
func (p *Page) context(ctx context.Context) (worldKey, int64, error) {
	k, err := p.root(ctx)
	if err != nil {
		return k, 0, err
	}
	if k == p.world && p.ctxID != 0 {
		return k, p.ctxID, nil
	}
	// A tab the user is not looking at is hidden: Chrome stops its animation frames, throttles timers to a second
	// and holds input acknowledgements for five. Focus emulation makes it run as if focused, without raising it.
	// It lasts for the debugger session, so set it again whenever a document's world is new.
	_ = p.call(ctx, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}, nil)
	var res struct {
		ExecutionContextID int64 `json:"executionContextId"`
	}
	if err := p.call(ctx, "Page.createIsolatedWorld", map[string]any{"frameId": k.frame, "worldName": worldName}, &res); err != nil {
		return k, 0, stale(err, "the page is navigating")
	}
	p.world, p.ctxID = k, res.ExecutionContextID
	return k, p.ctxID, nil
}

// eval evaluates expr in a world and decodes its value into out. It reports false for null or undefined. A thrown
// exception or a vanished world is a StaleError: the scripts only throw when the document changed under them.
func (p *Page) eval(ctx context.Context, ctxID int64, expr string, await bool, out any) (bool, error) {
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	params := map[string]any{"expression": expr, "contextId": ctxID, "returnByValue": true}
	if await {
		params["awaitPromise"] = true
	}
	if err := p.call(ctx, "Runtime.evaluate", params, &res); err != nil {
		var de *DetachedError
		if !errors.As(err, &de) && strings.Contains(strings.ToLower(err.Error()), "context") {
			p.ctxID = 0
			return false, &StaleError{"the page changed; observe it again"}
		}
		return false, err
	}
	if d := res.ExceptionDetails; d != nil {
		msg := d.Text
		if d.Exception != nil && d.Exception.Description != "" {
			msg = d.Exception.Description
		}
		return false, &StaleError{truncate(msg, 200)}
	}
	v := res.Result.Value
	if len(v) == 0 || string(v) == "null" {
		return false, nil
	}
	if out == nil {
		return true, nil
	}
	return true, json.Unmarshal(v, out)
}

// Observe reads the page as numbered controls, retrying while the document is being replaced.
func (p *Page) Observe(ctx context.Context) (*Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.observe(ctx)
}

func (p *Page) observe(ctx context.Context) (*Observation, error) {
	var last error = &StaleError{"the page did not settle"}
	for attempt := 0; attempt < 15; attempt++ {
		obs, err := p.snapshot(ctx)
		if err == nil && obs != nil {
			return obs, nil
		}
		if err != nil {
			var se *StaleError
			if !errors.As(err, &se) {
				return nil, err
			}
			last = err
		}
		if err := sleep(ctx, 150*time.Millisecond); err != nil {
			return nil, err
		}
	}
	return nil, last
}

func (p *Page) snapshot(ctx context.Context) (*Observation, error) {
	k, id, err := p.context(ctx)
	if err != nil {
		return nil, err
	}
	var raw rawSnap
	ok, err := p.eval(ctx, id, js.Snapshot, false, &raw)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil // the document is being replaced
	}
	obs := build(&raw)
	obs.world, obs.ctxID = k, id
	p.last = obs
	return obs, nil
}

// build turns one document's snapshot into an observation.
func build(r *rawSnap) *Observation {
	obs := &Observation{URL: r.URL, Title: r.Title, Scroll: r.Scroll, Omitted: r.Omitted, Marker: r.Marker,
		Groups: r.Groups, CrossOriginFrames: len(r.Frames), guards: map[int]json.RawMessage{},
		viewport: size{r.Viewport.Width, r.Viewport.Height}}
	// Cross-origin iframes leave marker lines ("\x00F<node>\x00") in the text; this engine does not read them.
	var lines []string
	for _, line := range strings.Split(r.Text, "\n") {
		if !strings.HasPrefix(line, "\x00F") {
			lines = append(lines, line)
		}
	}
	obs.Text = strings.Join(lines, "\n")
	obs.Elements = make([]Element, 0, len(r.Elements))
	for i, re := range r.Elements {
		e := re.Element
		e.Index = i + 1
		obs.Elements = append(obs.Elements, e)
		if g, ok := r.Guards[fmt.Sprint(e.Node)]; ok {
			obs.guards[e.Node] = g
		}
	}
	obs.Outline = outline(obs, outlineBudget)
	if obs.CrossOriginFrames > 0 {
		obs.Outline += fmt.Sprintf("\n(%d cross-origin frames not read)", obs.CrossOriginFrames)
	}
	return obs
}

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

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
