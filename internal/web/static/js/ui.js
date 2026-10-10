// ui.js — tiny DOM builder, toasts, modals, shared bits.

// h('div', {class:'x', onclick:fn}, child, ...) — strings become text nodes,
// so user data is never interpreted as HTML.
export function h(tag, props, ...children) {
  const el = document.createElement(tag);
  if (props) {
    for (const [k, v] of Object.entries(props)) {
      if (v === undefined || v === null) continue;
      if (k === 'class') el.className = v;
      else if (k === 'dataset') Object.assign(el.dataset, v);
      else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
      else if (k === 'value') el.value = v;
      else if (k === 'checked') el.checked = !!v;
      else if (k === 'disabled') el.disabled = !!v;
      else el.setAttribute(k, v);
    }
  }
  append(el, children);
  return el;
}

function append(el, children) {
  for (const c of children.flat(Infinity)) {
    if (c === null || c === undefined || c === false) continue;
    if (c instanceof Node) el.appendChild(c);
    else el.appendChild(document.createTextNode(String(c)));
  }
}

export function clear(el) { while (el.firstChild) el.removeChild(el.firstChild); return el; }

// --- toasts ---

let toastBox = null;
export function toast(msg, kind = 'info', ms = 4000) {
  if (!toastBox) {
    toastBox = h('div', { class: 'toasts' });
    document.body.appendChild(toastBox);
  }
  const t = h('div', { class: `toast toast-${kind}` }, msg);
  toastBox.appendChild(t);
  setTimeout(() => { t.classList.add('out'); setTimeout(() => t.remove(), 300); }, ms);
}

// --- modal ---

export function modal({ title, body, actions, wide }) {
  const overlay = h('div', { class: 'overlay', onclick: (e) => { if (e.target === overlay) close(); } });
  const box = h('div', { class: 'modal' + (wide ? ' modal-wide' : '') },
    h('div', { class: 'modal-head' },
      h('div', { class: 'modal-title' }, title),
      h('button', { class: 'btn btn-ghost', onclick: () => close() }, '✕')),
    h('div', { class: 'modal-body' }),
    actions && actions.length ? h('div', { class: 'modal-actions' }, actions) : null);
  const bodyEl = box.querySelector('.modal-body');
  if (body instanceof Node) bodyEl.appendChild(body);
  else if (body) bodyEl.appendChild(h('div', {}, body));
  document.body.appendChild(overlay);
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  document.addEventListener('keydown', onKey);
  function close() {
    document.removeEventListener('keydown', onKey);
    overlay.remove();
  }
  return { close, body: bodyEl, root: overlay };
}

export function confirmModal(message, { danger = true, okLabel = 'Confirm' } = {}) {
  return new Promise((resolve) => {
    const m = modal({
      title: 'Confirm',
      body: h('div', {}, message),
      actions: [
        h('button', { class: 'btn', onclick: () => { m.close(); resolve(false); } }, 'Cancel'),
        h('button', {
          class: 'btn ' + (danger ? 'btn-danger' : 'btn-primary'),
          onclick: () => { m.close(); resolve(true); },
        }, okLabel),
      ],
    });
  });
}

// --- shared bits ---

const STATUS_COLORS = {
  new: 'gray', added: 'gray', researching: 'purple', research: 'purple',
  planned: 'blue', ready: 'green', merged: 'green', applying: 'amber',
  error: 'red', conflicted: 'red',
};

export function statusBadge(status) {
  const s = (status || 'new').toLowerCase();
  return h('span', { class: 'badge badge-' + (STATUS_COLORS[s] || 'gray') }, s);
}

export function relTime(iso) {
  if (!iso || iso.startsWith('0001')) return 'never';
  const d = new Date(iso);
  const s = (Date.now() - d.getTime()) / 1000;
  if (s < 60) return 'just now';
  if (s < 3600) return Math.floor(s / 60) + 'm ago';
  if (s < 86400) return Math.floor(s / 3600) + 'h ago';
  return Math.floor(s / 86400) + 'd ago';
}

export function spinner() { return h('span', { class: 'spinner' }); }

export function emptyState(title, sub) {
  return h('div', { class: 'empty' },
    h('div', { class: 'empty-title' }, title),
    sub ? h('div', { class: 'empty-sub' }, sub) : null);
}
