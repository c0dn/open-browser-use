package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCheckSteps(t *testing.T) {
	ok := [][]Step{
		{{Op: "click", Index: 1}},
		{{Op: "type", Index: 2, Value: ""}, {Op: "press", Value: "Enter"}},
		{{Op: "check", Index: 3, Value: "false"}, {Op: "scroll"}, {Op: "wait", Value: "200"}, {Op: "back"}},
	}
	for _, steps := range ok {
		if err := CheckSteps(steps); err != nil {
			t.Errorf("%v: %v", steps, err)
		}
	}
	bad := map[string][]Step{
		"provide 1-20":     nil,
		"op must be":       {{Op: "eval"}},
		"needs an index":   {{Op: "click"}},
		"takes no index":   {{Op: "press", Index: 1, Value: "Enter"}},
		"keys are":         {{Op: "press", Value: "F13"}},
		"select needs":     {{Op: "select", Index: 1}},
		"true or false":    {{Op: "check", Index: 1, Value: "yes"}},
		"down or up":       {{Op: "scroll", Value: "left"}},
		"is milliseconds":  {{Op: "wait", Value: "-1"}},
		"provide 1-20 st…": make([]Step, MaxSteps+1),
	}
	for want, steps := range bad {
		err := CheckSteps(steps)
		var ie *InputError
		if !errors.As(err, &ie) || !strings.Contains(err.Error(), strings.TrimSuffix(want, " st…")) {
			t.Errorf("%s: got %v", want, err)
		}
	}
}

func TestBuildNumbersControlsAndDropsFrameMarkers(t *testing.T) {
	raw := &rawSnap{
		URL: "https://example.com/", Title: "Example",
		Text: "Before\n\x00F9\x00\nAfter",
		Elements: []rawElement{
			{Element: Element{Node: 4, Role: "textbox", Label: "Name", Ops: []string{"type", "click"}}},
			{Element: Element{Node: 7, Role: "button", Label: "Go", Ops: []string{"click"}}},
		},
		Guards: map[string]json.RawMessage{"4": json.RawMessage(`[4,"textbox"]`)},
		Frames: []json.RawMessage{json.RawMessage(`{"node":9}`)},
	}
	obs := build(raw)
	if obs.Text != "Before\nAfter" {
		t.Errorf("text = %q", obs.Text)
	}
	if obs.Elements[0].Index != 1 || obs.Elements[1].Index != 2 {
		t.Errorf("indices = %d, %d", obs.Elements[0].Index, obs.Elements[1].Index)
	}
	if string(obs.guards[4]) != `[4,"textbox"]` {
		t.Errorf("guard = %s", obs.guards[4])
	}
	for _, want := range []string{`[1] textbox "Name" (empty)`, `[2] button "Go"`, "1 cross-origin frames not read"} {
		if !strings.Contains(obs.Outline, want) {
			t.Errorf("outline lacks %q:\n%s", want, obs.Outline)
		}
	}
}

// fakeCDP answers the calls observe makes and records the rest.
type fakeCDP struct {
	calls  []string
	detach bool
}

func (f *fakeCDP) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	if f.detach {
		return nil, &DetachedError{"gone"}
	}
	switch method {
	case "Page.getFrameTree":
		return json.RawMessage(`{"frameTree":{"frame":{"id":"F","loaderId":"L"}}}`), nil
	case "Page.createIsolatedWorld":
		if p := params.(map[string]any); p["worldName"] != worldName {
			return nil, errors.New("wrong world")
		}
		return json.RawMessage(`{"executionContextId":5}`), nil
	case "Runtime.evaluate":
		p := params.(map[string]any)
		if p["contextId"] != int64(5) {
			return nil, errors.New("evaluated outside the isolated world")
		}
		return json.RawMessage(`{"result":{"value":{"url":"https://example.com/","title":"T","text":"",` +
			`"elements":[{"node":1,"role":"button","label":"Go","ops":["click"]}]}}}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func TestObserveUsesOneIsolatedWorldPerDocument(t *testing.T) {
	f := &fakeCDP{}
	p := NewPage(f)
	for range 2 {
		obs, err := p.Observe(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(obs.Elements) != 1 || obs.Elements[0].Label != "Go" {
			t.Fatalf("elements = %+v", obs.Elements)
		}
	}
	worlds := 0
	for _, c := range f.calls {
		if c == "Page.createIsolatedWorld" {
			worlds++
		}
		if c == "Runtime.enable" {
			t.Error("Runtime.enable is visible to pages")
		}
	}
	if worlds != 1 {
		t.Errorf("created %d worlds, want 1: %v", worlds, f.calls)
	}
}

func TestDetachedDebuggerIsNotRetried(t *testing.T) {
	f := &fakeCDP{detach: true}
	_, err := NewPage(f).Observe(context.Background())
	var de *DetachedError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("retried a detached tab: %v", f.calls)
	}
}

func TestActNeedsAnObservation(t *testing.T) {
	_, err := NewPage(&fakeCDP{}).Act(context.Background(), []Step{{Op: "click", Index: 1}})
	if err == nil || !strings.Contains(err.Error(), "observe the page first") {
		t.Fatalf("err = %v", err)
	}
}
