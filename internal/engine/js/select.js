// Choose a native <select> option by value or label, firing the events a person's choice would. From Maya.
((node, value) => {
  const e = window.__wayfarer?.nodes.get(node);
  if (!e?.isConnected || e.tagName !== 'SELECT') return false;
  const option = [...e.options].find(o => o.value === value || o.label === value);
  if (!option || option.disabled) return false;
  e.value = option.value;
  e.dispatchEvent(new Event('input', {bubbles: true}));
  e.dispatchEvent(new Event('change', {bubbles: true}));
  return true;
})
