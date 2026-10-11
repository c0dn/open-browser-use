// Resolve an observed node for input: re-check identity, visibility and occlusion; scroll it into view if needed.
// Ported from Maya's snapshot.ts TARGET. Coordinates are in this document's viewport; wayfarerd adds the offsets of
// any cross-origin iframes around it (frame.js).
((node, expected, op) => {
  const cache = window.__wayfarer;
  const e = cache?.nodes.get(node);
  if (!e?.isConnected) return {error: 'gone'};
  if (expected && JSON.stringify(cache.guard(e)) !== JSON.stringify(expected)) return {error: 'changed'};
  if (e.matches(':disabled') || e.closest('[aria-disabled="true"],[inert]')) return {error: 'disabled'};
  if (['type', 'password'].includes(op) && (e.readOnly || e.getAttribute('aria-readonly') === 'true')) return {error: 'readonly'};
  // Coordinates through any same-origin iframes the control is in.
  let r = cache.rectOf(e);
  if (r.y < 0 || r.y + r.height > innerHeight || r.x < 0 || r.x + r.width > innerWidth || !cache.inFrame(e, r.local)) {
    e.scrollIntoView({block: 'center', inline: 'center', behavior: 'instant'});
    r = cache.rectOf(e);
  }
  const x = r.x + r.width / 2, y = r.y + r.height / 2;
  if (!r.width || !r.height || x < 0 || y < 0 || x >= innerWidth || y >= innerHeight || !cache.inFrame(e, r.local)) return {error: 'offscreen'};
  // What a click there lands on, in the control's own document; then each iframe holding it must be on top in its parent.
  const topAt = (node, doc) => {
    const root = node.getRootNode(), o = cache.offsetOf(doc);
    return (root.elementFromPoint ? root : doc).elementFromPoint(x - o.x, y - o.y);
  };
  let doc = e.ownerDocument, hit = topAt(e, doc);
  let clear = hit && (hit === e || e.contains(hit) || hit.contains(e) && hit.tagName === 'LABEL');
  for (let frame = doc.defaultView.frameElement; clear && doc !== document && frame; frame = doc.defaultView.frameElement) {
    doc = frame.ownerDocument;
    clear = topAt(frame, doc) === frame;
  }
  if (op !== 'select' && op !== 'upload' && !clear) return {error: 'covered'};
  return {x, y, tag: e.tagName, type: e.type || null, origin: e.ownerDocument.location.origin};
})
