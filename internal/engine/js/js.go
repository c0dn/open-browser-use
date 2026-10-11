// Package js holds the scripts the engine runs in a document's isolated world. They are plain JavaScript, ported from
// Wayfarer's engine (itself from Maya's snapshot.ts and browser-use/jev-ultrafast's snapshot.js, MIT). Page scripts
// can neither see nor change them, and model output never becomes part of one (arguments are JSON-encoded).
package js

import _ "embed"

var (
	// Snapshot observes a document: an expression returning the snapshot, or null while the document is replaced.
	//go:embed snapshot.js
	Snapshot string
	// Target is a function (node, expected guard, op) resolving an observed node to a point to act at.
	//go:embed target.js
	Target string
	// Select is a function (node, value) choosing a <select> option.
	//go:embed select.js
	Select string
	// Settle is a function (node) returning a promise that resolves once the page has reacted.
	//go:embed settle.js
	Settle string
)
