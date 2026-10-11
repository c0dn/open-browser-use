// Observe one document (and the same-origin iframes inside it) as numbered controls, in Wayfarer's isolated world.
// Ported from Maya's snapshot.ts (itself from browser-use/jev-ultrafast's snapshot.js, MIT). Returns null while the
// document is replaced.
//
// Additions for Wayfarer: every control carries its centre (x, y) in this document's viewport; cross-origin iframes,
// which this world cannot read, are reported as `frames` placeholders (their content box, region and heading, and
// where their controls go in the list) and as a marker line in the text, so wayfarerd can merge the snapshot it takes
// inside each of them. Node ids are local to this world; wayfarerd maps them to page-wide ids.
(() => {
  if (!document.body) return null;
  const cache = window.__wayfarer ||= {ids: new WeakMap(), nodes: new Map(), next: 1};
  const identity = e => {
    if (!cache.ids.has(e)) cache.ids.set(e, cache.next++);
    const id = cache.ids.get(e); cache.nodes.set(id, e); return id;
  };
  for (const [id, e] of cache.nodes) if (!e.isConnected) cache.nodes.delete(id);
  const visible = e => !e.closest('[aria-hidden="true"],[inert]') &&
    e.checkVisibility({checkOpacity: true, checkVisibilityCSS: true});
  // Same-origin iframes: where each document's viewport sits in this one, so its controls get these coordinates.
  const frameOf = doc => doc === document ? null : doc.defaultView?.frameElement ?? null;
  const offsetOf = doc => {
    const f = frameOf(doc);
    if (!f) return {x: 0, y: 0};
    const o = offsetOf(f.ownerDocument), r = f.getBoundingClientRect(), st = getComputedStyle(f);
    return {x: o.x + r.x + f.clientLeft + parseFloat(st.paddingLeft), y: o.y + r.y + f.clientTop + parseFloat(st.paddingTop)};
  };
  const rectOf = e => {
    const r = e.getBoundingClientRect(), o = offsetOf(e.ownerDocument);
    return {x: r.x + o.x, y: r.y + o.y, width: r.width, height: r.height, local: r};
  };
  // Inside its own frame's viewport, as well as this document's.
  const inFrame = (e, r) => {
    const view = e.ownerDocument.defaultView, x = r.x + r.width / 2, y = r.y + r.height / 2;
    return x >= 0 && y >= 0 && x < view.innerWidth && y < view.innerHeight;
  };
  const docs = [], cross = [];
  const addDoc = doc => {
    docs.push(doc);
    for (const f of doc.querySelectorAll('iframe,frame')) {
      let inner = null;
      try { inner = f.contentDocument; } catch {}
      if (inner?.body) {
        if (!docs.includes(inner) && visible(f)) addDoc(inner);
      } else if (!inner && visible(f)) {
        cross.push(f); // another origin: wayfarerd reads it in its own world
      }
    }
  };
  addDoc(document);
  const frameDocs = new Set(docs), crossSet = new Set(cross);
  cache.offsetOf = offsetOf; cache.rectOf = rectOf; cache.inFrame = inFrame;
  const byId = (e, id) => e.getRootNode().getElementById?.(id) ?? document.getElementById(id);
  const name = (e, seen = new Set()) => {
    if (!e || seen.has(e)) return '';
    seen.add(e);
    const referenced = (e.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean)
      .map(id => name(byId(e, id), seen)).filter(Boolean).join(' ');
    return (referenced || e.getAttribute('aria-label') ||
      [...(e.labels || [])].map(l => name(l, seen)).filter(Boolean).join(' ') ||
      (['button', 'submit', 'reset'].includes(e.type) ? e.value : '') || e.getAttribute('alt') ||
      (e.tagName === 'INPUT' ? '' : [...e.childNodes].map(n => n.nodeType === 3 ? n.textContent :
        n.nodeType === 1 && n.getAttribute('aria-hidden') !== 'true' ? name(n, seen) : '').join(' ').trim()) ||
      e.getAttribute('title') || e.getAttribute('placeholder') || e.getAttribute('name') || '').replace(/\s+/g, ' ').trim().slice(0, 200);
  };
  const roles = ['button', 'link', 'checkbox', 'radio', 'switch', 'tab', 'menuitem', 'menuitemradio', 'menuitemcheckbox',
    'option', 'gridcell', 'combobox', 'textbox', 'searchbox', 'spinbutton', 'slider', 'treeitem'];
  const selector = 'a[href],button,input,textarea,select,summary,[contenteditable="true"],' +
    roles.map(role => '[role="' + role + '"]').join(',');
  const role = e => {
    if (e.__wayfarerClickable) return e.tagName === 'TR' ? 'row' : 'clickable';
    const explicit = e.getAttribute('role');
    if (roles.includes(explicit)) return explicit;
    if (e.tagName === 'BUTTON' || e.tagName === 'SUMMARY') return 'button';
    if (e.tagName === 'A') return 'link';
    if (e.tagName === 'SELECT') return 'combobox';
    if (e.tagName === 'TEXTAREA' || e.isContentEditable) return 'textbox';
    if (e.tagName === 'INPUT') {
      if (['checkbox', 'radio'].includes(e.type)) return e.type;
      if (['button', 'submit', 'reset', 'image'].includes(e.type)) return 'button';
      if (e.type === 'search') return 'searchbox';
      if (e.type === 'number') return 'spinbutton';
      if (e.type === 'range') return 'slider';
      if (e.type === 'password') return 'password';
      if (e.type === 'file') return 'file';
      if (['text', 'email', 'url', 'tel', '', 'date', 'datetime-local', 'time', 'month', 'week'].includes(e.type)) return 'textbox';
    }
    return null;
  };
  // Controls in the document and in open shadow roots.
  const found = [];
  const collect = root => {
    for (const e of root.querySelectorAll(selector)) found.push(e);
    for (const host of root.querySelectorAll('*')) if (host.shadowRoot) collect(host.shadowRoot);
  };
  for (const doc of docs) collect(doc);
  // Clickable things that aren't controls: table rows, cards and list items made clickable by a script (a pointer
  // cursor, no link or button inside). The outermost such element counts once.
  const controlSel = selector + ',label';
  const pointer = e => getComputedStyle(e).cursor === 'pointer';
  let scanned = 0;
  const likely = docs.flatMap(doc => [...doc.querySelectorAll('tr,li,[onclick],[tabindex]:not([tabindex="-1"]),article')]);
  const seenLikely = new Set(likely);
  const rest = docs.flatMap(doc => [...doc.querySelectorAll('div,td,span,section')]).filter(e => !seenLikely.has(e));
  for (const e of [...likely, ...rest]) {
    if (++scanned > 4000) break;
    e.__wayfarerClickable = false;
    if (e.matches(controlSel) || e.closest(controlSel) || !visible(e)) continue;
    if (!pointer(e) || e.querySelector(controlSel)) continue;
    if (e.parentElement && pointer(e.parentElement) && !e.parentElement.matches('body,html')) continue;
    if (!(e.innerText || '').trim()) continue;
    e.__wayfarerClickable = true;
    found.push(e);
  }
  const guard = e => {
    if (!e?.isConnected || !visible(e)) return null;
    const scope = e.closest('form,dialog,[role="dialog"],li,tr,[role="row"]') || e.parentElement;
    return [identity(e), role(e), name(e), e.matches(':disabled'), e.getAttribute('aria-disabled'),
      e.getAttribute('href'), (scope?.innerText || '').slice(0, 500)];
  };
  cache.guard = guard;
  // Regions: the nearest form, dialog, row or landmark a control sits in, so a reader sees which fields belong together.
  const REGION = 'dialog,[role="dialog"],[role="alertdialog"],form,fieldset,[role="row"],tr,li,article,nav,header,footer,aside,main,[role="search"],[role="navigation"],[role="main"],[role="tabpanel"],[role="menu"],[role="listbox"]';
  const regionIds = new Map(), groups = [];
  const regionKind = g => {
    const r = g.getAttribute('role');
    if (r === 'alertdialog') return 'dialog';
    if (r) return r === 'navigation' ? 'nav' : r;
    return {TR: 'row', LI: 'item', ARTICLE: 'item', FIELDSET: 'fieldset', DIALOG: 'dialog'}[g.tagName] || g.tagName.toLowerCase();
  };
  const regionLabel = g => (g.getAttribute('aria-label') ||
    (g.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean).map(id => byId(g, id)?.innerText || '').join(' ') ||
    g.querySelector(':scope > legend, h1, h2, h3, h4, [role="heading"]')?.innerText || g.getAttribute('name') || '')
    .replace(/\s+/g, ' ').trim().slice(0, 120);
  const region = e => {
    const g = e.parentElement?.closest(REGION);
    // A control in an iframe belongs to the region holding the iframe (the dialog it fills).
    if (!g) return frameOf(e.ownerDocument) ? region(frameOf(e.ownerDocument)) : 0;
    if (!regionIds.has(g)) {
      // The enclosing region first, so ids are assigned outside-in and never collide.
      const parent = region(g), id = groups.length + 1, kind = regionKind(g);
      regionIds.set(g, id);
      // Rows and items carry their text (a search result's snippet, a table row's cells).
      const text = ['row', 'item'].includes(kind) ? (g.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 200) : '';
      groups.push({id, kind, label: regionLabel(g), ...(text ? {text} : {}), ...(parent ? {parent} : {})});
    }
    return regionIds.get(g);
  };
  // The nearest heading before a control: where on the page it is.
  const headingsOf = new Map(docs.map(doc => [doc, [...doc.querySelectorAll('h1,h2,h3,h4,[role="heading"]')].filter(h => visible(h))]));
  const headingOf = e => {
    const headings = headingsOf.get(e.ownerDocument) || [];
    let lo = 0, hi = headings.length - 1, best = null;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (headings[mid].compareDocumentPosition(e) & Node.DOCUMENT_POSITION_FOLLOWING) { best = headings[mid]; lo = mid + 1; } else hi = mid - 1;
    }
    return best ? best.innerText.replace(/\s+/g, ' ').trim().slice(0, 100) : '';
  };
  const elements = [];
  // Every visible control on the page, below the fold too (marked offscreen); on-screen ones are kept first if over the cap.
  const candidates = [];
  for (const e of found) {
    if (e.type === 'hidden' || !visible(e) || e.matches(':disabled') || e.closest('[aria-disabled="true"]')) continue;
    const r = rectOf(e), x = r.x + r.width / 2, y = r.y + r.height / 2, kind = role(e);
    if (!kind || r.width <= 0 || r.height <= 0) continue;
    if (kind === 'gridcell' && e.querySelector('button,[role="button"]')) continue;
    candidates.push({e, kind, x, y, offscreen: x < 0 || y < 0 || x >= innerWidth || y >= innerHeight || !inFrame(e, r.local)});
  }
  const LIMIT = 400;
  let omitted = 0;
  if (candidates.length > LIMIT) {
    const keep = new Set([...candidates].sort((a, b) => a.offscreen - b.offscreen).slice(0, LIMIT));
    omitted = candidates.length - LIMIT;
    candidates.splice(0, candidates.length, ...candidates.filter(c => keep.has(c)));
  }
  for (const {e, kind, x, y, offscreen} of candidates) {
    const item = {node: identity(e), role: kind, label: name(e) || kind, ops: [], x, y};
    if (offscreen) item.offscreen = true;
    if (kind === 'link' && e.getAttribute('href')) item.href = e.getAttribute('href').slice(0, 500);
    const group = region(e);
    if (group) item.group = group;
    const heading = headingOf(e);
    if (heading) item.heading = heading;
    if (e.required || e.getAttribute('aria-required') === 'true') item.required = true;
    if (e.getAttribute('aria-invalid') === 'true' || (e.willValidate && e.value && e.validity && !e.validity.valid)) {
      item.invalid = (e.validationMessage || 'invalid').slice(0, 120);
    }
    const described = (e.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean)
      .map(id => byId(e, id)?.innerText || '').join(' ').replace(/\s+/g, ' ').trim();
    if (described) item.description = described.slice(0, 160);
    if (e.getAttribute('placeholder') && e.getAttribute('placeholder') !== item.label) item.placeholder = e.getAttribute('placeholder').slice(0, 80);
    for (const key of ['checked', 'selected', 'expanded']) {
      const value = e.getAttribute('aria-' + key);
      if (value !== null) item[key] = value;
    }
    if (['checkbox', 'radio'].includes(e.type)) item.checked = String(e.checked);
    if (kind === 'password') {
      item.ops.push('password');
      item.value = e.value ? '(filled)' : '';
    } else if (kind === 'file') {
      item.ops.push('upload');
      item.value = [...(e.files || [])].map(f => f.name).join(', ');
    } else if (e.tagName === 'SELECT') {
      item.ops.push('select');
      item.value = [...e.selectedOptions].map(o => o.label).join(', ');
      item.options = [...e.options].filter(o => !o.disabled && !o.closest('optgroup[disabled]'))
        .slice(0, 50).map(o => ({label: o.label.slice(0, 100), value: o.value}));
    } else {
      const editable = !e.readOnly && e.getAttribute('aria-readonly') !== 'true' &&
        (['textbox', 'searchbox', 'spinbutton'].includes(kind) || (kind === 'combobox' && ['INPUT', 'TEXTAREA'].includes(e.tagName)));
      item.value = (['row', 'clickable'].includes(kind) ? '' : 'value' in e ? String(e.value) : e.isContentEditable || kind === 'combobox' ? e.innerText.trim() : '').slice(0, 200);
      if (editable) item.ops.push('type');
      item.ops.push('click');
    }
    if (e.getAttribute('autocomplete')) item.autocomplete = e.getAttribute('autocomplete');
    if (e.type && e.tagName === 'INPUT') item.type = e.type;
    item.__e = e;
    elements.push(item);
  }
  // Cross-origin iframes: their content box here, the region and heading their controls inherit, and their place in
  // the list (before the first control that follows them in the document).
  const frames = cross.map(f => {
    const r = rectOf(f), st = getComputedStyle(f);
    const at = elements.findIndex(item => item.__e.ownerDocument === f.ownerDocument &&
      (f.compareDocumentPosition(item.__e) & Node.DOCUMENT_POSITION_FOLLOWING));
    const box = {node: identity(f), x: r.x + f.clientLeft + parseFloat(st.paddingLeft), y: r.y + f.clientTop + parseFloat(st.paddingTop),
      width: f.clientWidth, height: f.clientHeight, at: at < 0 ? elements.length : at};
    const group = region(f), heading = headingOf(f);
    if (group) box.group = group;
    if (heading) box.heading = heading;
    return box;
  });
  for (const item of elements) delete item.__e;
  // Visible text only: offscreen bodies and footers don't fill the model's context.
  // Text in one block stays on one line: a search highlight (<mark>GROW</mark>-Pro) must not split a word.
  // Same-origin iframes' text sits where the iframe is; a cross-origin iframe leaves a marker line for wayfarerd.
  const lines = []; let length = 0, lastBlock = null;
  const blockOf = e => {
    for (let depth = 0; e && depth < 12; e = e.parentElement, depth++) {
      if (!getComputedStyle(e).display.startsWith('inline')) return e;
    }
    return e;
  };
  const readText = doc => {
    const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
    const range = doc.createRange(), view = doc.defaultView, o = offsetOf(doc); let node;
    while ((node = walker.nextNode()) && length < 8000) {
      if (node.nodeType === 1) {
        if (/^I?FRAME$/.test(node.tagName)) {
          let inner = null;
          try { inner = node.contentDocument; } catch {}
          if (frameDocs.has(inner)) { readText(inner); lastBlock = null; }
          else if (crossSet.has(node)) { lines.push('\u0000F' + cache.ids.get(node) + '\u0000'); lastBlock = null; }
        }
        continue;
      }
      const raw = node.textContent.replace(/\s+/g, ' '), parent = node.parentElement;
      if (!raw.trim() || !parent || parent.closest('script,style,noscript,template') || !visible(parent)) continue;
      range.selectNodeContents(node); const r = range.getBoundingClientRect();
      if (r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < view.innerHeight && r.right > 0 && r.left < view.innerWidth &&
          r.bottom + o.y > 0 && r.top + o.y < innerHeight && r.right + o.x > 0 && r.left + o.x < innerWidth) {
        const block = blockOf(parent);
        if (block === lastBlock && lines.length) lines[lines.length - 1] += raw;
        else lines.push(raw);
        lastBlock = block; length += raw.length;
      }
    }
  };
  readText(document);
  const text = lines.map(line => line.trim()).filter(Boolean).join('\n').slice(0, 8000);
  const guards = {};
  for (const item of elements) guards[item.node] = guard(cache.nodes.get(item.node));
  const height = document.documentElement.scrollHeight;
  return {url: location.href, title: document.title, text, elements, omitted, guards, groups, frames,
    scroll: {y: Math.round(scrollY), height, viewport: innerHeight}, viewport: {width: innerWidth, height: innerHeight},
    marker: [performance.timeOrigin, location.href, document.title, text.length, elements.length]};
})()
