package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Ported from Wayfarer's engine.

// outlineBudget is the outline's size in characters before regions and offscreen controls are cut (Maya's 14 000).
const outlineBudget = 14_000

// quote is JSON.stringify for a string: JSON without HTML escaping.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// control renders one control as a line: "[3] textbox "Name" = "William" required".
func control(e Element) string {
	var state []string
	add := func(s string) {
		if s != "" {
			state = append(state, s)
		}
	}
	switch {
	case e.Role == "password":
		add(map[bool]string{true: "(filled)", false: "(empty)"}[e.Value != ""])
	case e.Value != "":
		add("= " + quote(truncate(e.Value, 80)))
	case slices.Contains(e.Ops, "type"):
		add("(empty)")
	}
	if e.Required {
		add("required")
	}
	if e.Invalid != "" {
		add("invalid: " + quote(e.Invalid))
	}
	if e.Checked != "" {
		add("checked=" + e.Checked)
	}
	if e.Expanded != "" {
		add("expanded=" + e.Expanded)
	}
	if e.Selected != "" {
		add("selected=" + e.Selected)
	}
	if e.Placeholder != "" {
		add("placeholder " + quote(e.Placeholder))
	}
	if e.Description != "" {
		add("hint " + quote(e.Description))
	}
	if len(e.Options) > 0 {
		var labels []string
		for _, o := range e.Options[:min(15, len(e.Options))] {
			labels = append(labels, o.Label)
		}
		more := ""
		if len(e.Options) > 15 {
			more = " …"
		}
		add("options: " + strings.Join(labels, " | ") + more)
	}
	if e.Offscreen {
		add("(below)")
	}
	line := fmt.Sprintf("[%d] %s %s", e.Index, e.Role, quote(e.Label))
	if len(state) > 0 {
		line += " " + strings.Join(state, " ")
	}
	return line
}

var noise = map[string]bool{"nav": true, "header": true, "footer": true, "aside": true, "navigation": true}

// outline renders an observation within budget characters: controls under the regions they sit in, with headings and
// state. Over budget, nav/header/footer regions and offscreen controls are cut first. Numbers are the observed
// indices; nothing here becomes a selector, and password values never appear.
func outline(obs *Observation, budget int) string {
	groups := map[int]*Group{}
	for i := range obs.Groups {
		groups[obs.Groups[i].ID] = &obs.Groups[i]
	}
	title := func(g *Group) string {
		if g.Label != "" {
			return g.Kind + " " + quote(g.Label)
		}
		return g.Kind
	}
	path := func(id int) []*Group {
		var chain []*Group
		g := groups[id]
		for depth := 0; g != nil && depth < 8; depth++ {
			chain = append([]*Group{g}, chain...)
			g = groups[g.Parent]
		}
		return chain
	}
	render := func(elements []Element) string {
		var lines []string
		heading := ""
		var open []*Group
		for _, e := range elements {
			if e.Heading != "" && e.Heading != heading {
				heading = e.Heading
				lines = append(lines, "# "+heading)
				open = nil
			}
			chain := path(e.Group)
			shared := 0
			for shared < len(chain) && shared < len(open) && chain[shared] == open[shared] {
				shared++
			}
			for depth := shared; depth < len(chain); depth++ {
				g := chain[depth]
				line := strings.Repeat("  ", depth) + title(g)
				if g.Text != "" && g.Text != g.Label {
					line += ": " + quote(truncate(g.Text, 160))
				}
				lines = append(lines, line)
			}
			open = chain
			lines = append(lines, strings.Repeat("  ", len(chain))+control(e))
		}
		return strings.Join(lines, "\n")
	}
	noisy := func(e Element) bool {
		return slices.ContainsFunc(path(e.Group), func(g *Group) bool { return noise[g.Kind] })
	}
	tiers := []func(Element) bool{
		func(Element) bool { return true },
		func(e Element) bool { return !(noisy(e) && e.Offscreen) },
		func(e Element) bool { return !noisy(e) },
		func(e Element) bool {
			return !noisy(e) && (!e.Offscreen || slices.ContainsFunc(e.Ops, func(op string) bool { return op != "click" }))
		},
		func(e Element) bool { return !e.Offscreen },
	}
	filter := func(keep func(Element) bool) []Element {
		var out []Element
		for _, e := range obs.Elements {
			if keep(e) {
				out = append(out, e)
			}
		}
		return out
	}
	prefix := ""
	for _, keep := range tiers {
		kept := filter(keep)
		if text := render(kept); len([]rune(text)) <= budget {
			return prefix + text + note(len(obs.Elements)-len(kept)+obs.Omitted)
		}
	}
	text := truncate(render(filter(func(e Element) bool { return !e.Offscreen })), budget)
	return prefix + text + "\n…" + note(obs.Omitted)
}

func note(left int) string {
	if left > 0 {
		return fmt.Sprintf("\n(%d more controls not listed; scroll or narrow down)", left)
	}
	return ""
}

// Control renders one control as the outline does: "[3] textbox "Name" = "William" required".
func Control(e Element) string { return control(e) }
