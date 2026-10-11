// Wait briefly for the page to react: two frames, or up to 250 ms for autocomplete options to appear. From Maya.
((node) => new Promise(resolve => {
  const field = window.__wayfarer?.nodes.get(node);
  const autocomplete = field?.getAttribute?.('role') === 'combobox' || field?.getAttribute?.('aria-autocomplete');
  let frames = 0, stopped = false;
  const finish = () => { stopped = true; resolve(true); };
  setTimeout(finish, autocomplete ? 250 : 60);
  const ready = () => {
    if (stopped) return;
    if (++frames >= 2 && !autocomplete) finish();
    else if (autocomplete && document.querySelector('[role="option"],[role="listbox"]')) finish();
    else requestAnimationFrame(ready);
  };
  requestAnimationFrame(ready);
}))
