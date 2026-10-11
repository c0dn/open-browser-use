package engine

import "encoding/json"

// maxElements caps the controls in one observation (Maya's LIMIT; snapshot.js applies it).
const maxElements = 400

// Observation is a page as numbered controls. Indices refer to this observation; node ids are stable while a
// document stays loaded, so a control keeps its id across observations.
type Observation struct {
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Text     string    `json:"text"`
	Elements []Element `json:"elements"`
	// Omitted counts controls left out over the cap (400, on-screen ones first).
	Omitted int     `json:"omitted"`
	Scroll  Scroll  `json:"scroll"`
	Groups  []Group `json:"groups,omitempty"`
	// CrossOriginFrames counts visible cross-origin iframes whose controls are not listed.
	CrossOriginFrames int `json:"crossOriginFrames,omitempty"`
	// Outline is the page as a model reads it: controls under their regions and headings, with state.
	Outline string `json:"outline"`
	// Marker changes when the page does (a load, a new address or title, different text or controls).
	Marker json.RawMessage `json:"marker,omitempty"`

	guards   map[int]json.RawMessage
	viewport size
	world    worldKey
	ctxID    int64
}

// Element is one control.
type Element struct {
	Index        int      `json:"index"`
	Node         int      `json:"node"`
	Role         string   `json:"role"`
	Label        string   `json:"label"`
	Ops          []string `json:"ops"`
	Value        string   `json:"value,omitempty"`
	Checked      string   `json:"checked,omitempty"`
	Selected     string   `json:"selected,omitempty"`
	Expanded     string   `json:"expanded,omitempty"`
	Options      []Option `json:"options,omitempty"`
	Autocomplete string   `json:"autocomplete,omitempty"`
	Type         string   `json:"type,omitempty"`
	// Offscreen is below or beside the viewport; acting on it scrolls it into view.
	Offscreen   bool   `json:"offscreen,omitempty"`
	Group       int    `json:"group,omitempty"`
	Heading     string `json:"heading,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Invalid     string `json:"invalid,omitempty"`
	Description string `json:"description,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Href        string `json:"href,omitempty"`
}

// Option is a <select> option.
type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Group is a form, dialog, row, list item or landmark that groups controls.
type Group struct {
	ID     int    `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Text   string `json:"text,omitempty"`
	Parent int    `json:"parent,omitempty"`
}

// Scroll is the page's vertical scroll position.
type Scroll struct {
	Y        float64 `json:"y"`
	Height   float64 `json:"height"`
	Viewport float64 `json:"viewport"`
}

type size struct{ Width, Height float64 }

// rawSnap is what snapshot.js returns for one document.
type rawSnap struct {
	URL      string                     `json:"url"`
	Title    string                     `json:"title"`
	Text     string                     `json:"text"`
	Elements []rawElement               `json:"elements"`
	Omitted  int                        `json:"omitted"`
	Guards   map[string]json.RawMessage `json:"guards"`
	Groups   []Group                    `json:"groups"`
	Frames   []json.RawMessage          `json:"frames"`
	Marker   json.RawMessage            `json:"marker"`
	Scroll   Scroll                     `json:"scroll"`
	Viewport struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"viewport"`
}

type rawElement struct {
	Element
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
