(() => {
'use strict';

// ---------- auth ----------
const qp = new URLSearchParams(location.search);
if (qp.get('token')) sessionStorage.setItem('maia_token', qp.get('token'));
let TOKEN = sessionStorage.getItem('maia_token') || '';

async function api(path, opts = {}) {
  const url = new URL(path, location.origin);
  if (TOKEN) url.searchParams.set('token', TOKEN);
  const res = await fetch(url, opts);
  if (res.status === 401) {
    const t = prompt('maia web token required:');
    if (t) { sessionStorage.setItem('maia_token', t); TOKEN = t; return api(path, opts); }
    throw new Error('unauthorized');
  }
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error || res.statusText);
  return body;
}

const $ = (id) => document.getElementById(id);

// ---------- state ----------
let st = null;            // /api/state
let wts = [];             // worktrees
let sel = null;           // selected slug
let tab = 'change';
let dirty = false;
let jobs = [];            // job summaries
let es = null;            // active EventSource
let watching = null;      // job id being streamed
let buffers = {};         // jobId -> [{id, cls, text}]
let lastIds = {};         // jobId -> last event id received
let changeLoadedFor = null;
let pendingSelect = null; // slug to auto-select once it appears in the list

// ---------- helpers ----------
function esc(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function toast(msg, isErr) {
  const el = document.createElement('div');
  el.className = 'toast' + (isErr ? ' err' : '');
  el.textContent = msg;
  document.body.appendChild(el);
  setTimeout(() => el.remove(), 3500);
}

function modal({ title, text, value, placeholder, okLabel }) {
  return new Promise((resolve) => {
    $('modal-title').textContent = title;
    $('modal-text').textContent = text || '';
    $('modal-text').classList.toggle('hidden', !text);
    const input = $('modal-input');
    input.classList.toggle('hidden', value === undefined);
    input.value = value || '';
    input.placeholder = placeholder || '';
    $('modal-ok').textContent = okLabel || 'OK';
    $('modal-backdrop').classList.remove('hidden');
    if (value !== undefined) setTimeout(() => input.focus(), 30);

    const done = (v) => {
      $('modal-backdrop').classList.add('hidden');
      $('modal-ok').onclick = $('modal-cancel').onclick = input.onkeydown = null;
      resolve(v);
    };
    $('modal-ok').onclick = () => done(input.classList.contains('hidden') ? true : (input.value.trim() || false));
    $('modal-cancel').onclick = () => done(false);
    input.onkeydown = (e) => { if (e.key === 'Enter') $('modal-ok').click(); if (e.key === 'Escape') done(false); };
  });
}

const current = () => wts.find((w) => w.slug === sel);

// ---------- refresh ----------
async function refresh() {
  try {
    const [state, list] = await Promise.all([api('/api/state'), api('/api/worktrees')]);
    st = state;
    wts = list;
  } catch (e) {
    return; // transient (e.g. server restarting)
  }

  $('repo-name').textContent = st.repo.name;
  $('model-badge').textContent = st.config.model || 'no model';
  $('key-missing').classList.toggle('hidden', st.config.has_api_key);

  const running = st.running_job;
  $('running-pill').classList.toggle('hidden', !running);
  if (running) $('running-label').textContent = `${running.kind} · ${running.slug}`;

  if (sel && !wts.some((w) => w.slug === sel)) {
    sel = null;
    $('detail').classList.add('hidden');
    $('welcome').classList.remove('hidden');
  }
  renderList();
  if (sel) renderDetail();
  await refreshJobs();

  if (pendingSelect && wts.some((w) => w.slug === pendingSelect)) {
    const s = pendingSelect;
    pendingSelect = null;
    select(s);
  }
}

async function refreshJobs() {
  try { jobs = await api('/api/jobs'); } catch (e) { return; }
  const selEl = $('job-select');
  const keep = selEl.value;
  selEl.innerHTML = '';
  for (const j of jobs) {
    const o = document.createElement('option');
    o.value = j.id;
    o.textContent = `${j.kind} · ${j.slug} — ${j.status}${j.status === 'running' ? '' : ` (${j.exit_code})`}`;
    selEl.appendChild(o);
  }
  if (watching && jobs.some((j) => j.id === watching)) selEl.value = watching;
  else if (keep && jobs.some((j) => j.id === keep)) selEl.value = keep;
  const run = jobs.find((j) => j.status === 'running');
  $('btn-cancel').disabled = !run || run.id !== watching;
}

// ---------- sidebar ----------
function renderList() {
  const ul = $('wt-list');
  ul.innerHTML = '';
  $('empty-list').classList.toggle('hidden', wts.length > 0);
  for (const w of wts) {
    const li = document.createElement('li');
    if (w.slug === sel) li.classList.add('selected');
    const dot = document.createElement('span');
    dot.className = 'dot ' + (w.status || 'new');
    dot.textContent = '●';
    const slugEl = document.createElement('div');
    slugEl.className = 'slug';
    slugEl.appendChild(dot);
    slugEl.appendChild(document.createTextNode(w.slug));
    li.appendChild(slugEl);
    if (w.description) {
      const d = document.createElement('div');
      d.className = 'desc';
      d.textContent = w.description;
      li.appendChild(d);
    }
    li.onclick = () => select(w.slug);
    ul.appendChild(li);
  }
}

function select(slug) {
  if (dirty) saveChange();
  sel = slug;
  changeLoadedFor = null;
  $('welcome').classList.add('hidden');
  $('detail').classList.remove('hidden');
  renderList();
  renderDetail();
  switchTab(tab === 'console' ? 'console' : tab);
}

// ---------- detail ----------
function renderDetail() {
  const w = current();
  if (!w) return;
  $('d-slug').textContent = w.slug;
  const s = $('d-status');
  s.textContent = w.status || 'new';
  s.className = 'status ' + (w.status || 'new');
  updateActions();
}

function updateActions() {
  const w = current();
  if (!w) return;
  const busy = !!(st && st.running_job);
  const mine = busy && st.running_job.slug === w.slug;
  const dis = (el, off) => { el.disabled = off; };
  const btn = (act) => document.querySelector(`[data-act="${act}"]`);
  dis(btn('init'), !w.has_change || busy);
  dis(btn('plan'), !w.has_research || busy);
  dis(btn('apply'), !w.has_plan || busy);
  dis(btn('dryrun'), !w.has_plan || busy);
  dis(btn('commit'), busy);
  dis(btn('merge'), busy || !w.has_plan);
  dis(btn('cleanup'), busy);
  $('steer-input').disabled = busy || !(w.has_plan || w.has_research);
  $('btn-steer').disabled = $('steer-input').disabled;
  if (mine) { /* keep buttons disabled while my job runs */ }
}

// ---------- tabs ----------
function switchTab(name) {
  tab = name;
  document.querySelectorAll('.tabs button').forEach((b) =>
    b.classList.toggle('active', b.dataset.tab === name));
  ['change', 'research', 'plan', 'diff', 'console'].forEach((t) =>
    $('tab-' + t).classList.toggle('hidden', t !== name));
  $('steer-bar').classList.toggle('hidden', !(name === 'plan' || name === 'research'));
  loadTab(name);
}

async function loadTab(name) {
  const w = current();
  if (!w) return;
  if (name === 'change') {
    if (changeLoadedFor !== w.slug && !dirty) {
      try {
        const d = await api(`/api/worktrees/${w.slug}/doc/change`);
        $('change-editor').value = d.raw;
        changeLoadedFor = w.slug;
        dirty = false;
        $('save-state').textContent = '';
      } catch (e) { /* no change.md */ }
    }
  } else if (name === 'research' || name === 'plan') {
    const panel = $('tab-' + name);
    try {
      const d = await api(`/api/worktrees/${w.slug}/doc/${name}`);
      panel.innerHTML = `<div class="md">${d.html}</div>`;
      panel.querySelectorAll('a').forEach((a) => { a.target = '_blank'; a.rel = 'noopener'; });
    } catch (e) {
      panel.innerHTML = `<div class="doc-empty">${name === 'plan'
        ? 'No plan yet — run <b>Research</b> first, then <b>Plan</b>.'
        : 'No research yet — run <b>Research</b> (<code>maia init</code>).'}</div>`;
    }
  } else if (name === 'diff') {
    await loadDiff();
  } else if (name === 'console') {
    if (!watching) {
      const latest = jobs.find((j) => j.slug === w.slug) || jobs[0];
      if (latest) watchJob(latest.id, false);
    }
    renderConsole();
  }
}

async function loadDiff() {
  const w = current();
  const panel = $('tab-diff');
  try {
    const d = await api(`/api/worktrees/${w.slug}/diff`);
    const parts = [];
    const block = (label, text, cls) => {
      if (!text.trim()) return;
      parts.push(`<div class="file">${label}</div><pre>${colorDiff(text, cls)}</pre>`);
    };
    block('staged', d.staged, 'staged');
    block('unstaged', d.unstaged, 'unstaged');
    if (d.untracked.length) {
      parts.push('<div class="file">untracked</div><pre>' +
        d.untracked.map((f) => `<span class="hunk">?? ${esc(f)}</span>`).join('\n') + '</pre>');
    }
    panel.innerHTML = parts.length
      ? parts.join('\n')
      : '<div class="doc-empty">No changes in this worktree.</div>';
  } catch (e) {
    panel.innerHTML = `<div class="doc-empty">${esc(e.message)}</div>`;
  }
}

function colorDiff(text) {
  return text.split('\n').map((line) => {
    const e = esc(line);
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') ||
        line.startsWith('index ') || line.startsWith('new file') || line.startsWith('deleted file'))
      return `<span class="hunk">${e}</span>`;
    if (line.startsWith('@@')) return `<span class="hunk">${e}</span>`;
    if (line.startsWith('+')) return `<span class="add">${e}</span>`;
    if (line.startsWith('-')) return `<span class="del">${e}</span>`;
    return e;
  }).join('\n');
}

// ---------- change.md editor ----------
async function saveChange() {
  const w = current();
  if (!w || !dirty) return;
  try {
    await api(`/api/worktrees/${w.slug}/doc/change`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ raw: $('change-editor').value }),
    });
    dirty = false;
    $('save-state').textContent = 'saved ✓';
    refresh();
  } catch (e) {
    toast('Save failed: ' + e.message, true);
  }
}

// ---------- actions ----------
async function runJob(payload, opts = {}) {
  try {
    const job = await api('/api/jobs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (opts.selectOnRefresh) pendingSelect = opts.selectOnRefresh;
    watchJob(job.id, true);
    return job;
  } catch (e) {
    if (e.message.includes('already running')) {
      const r = st && st.running_job;
      if (r) { watchJob(r.id, true); toast('Another command is running — showing it.', true); return; }
    }
    toast(e.message, true);
    return null;
  }
}

async function handleAction(act) {
  const w = current();
  if (!w) return;
  switch (act) {
    case 'init':
      runJob({ cmd: 'init', slug: w.slug });
      break;
    case 'plan':
      runJob({ cmd: 'plan', slug: w.slug });
      break;
    case 'dryrun': {
      const ok = await modal({
        title: 'Dry run apply',
        text: `Execute the plan for "${w.slug}" without writing files?`,
        okLabel: 'Run dry run',
      });
      if (ok) runJob({ cmd: 'apply', slug: w.slug, dry_run: true });
      break;
    }
    case 'apply': {
      const ok = await modal({
        title: 'Apply plan',
        text: `Execute the plan for "${w.slug}"? Files in the worktree will be modified.`,
        okLabel: 'Apply',
      });
      if (ok) runJob({ cmd: 'apply', slug: w.slug });
      break;
    }
    case 'commit': {
      const msg = await modal({
        title: 'Commit changes',
        text: `Stage and commit all changes in "${w.slug}".`,
        value: '',
        placeholder: 'commit message',
        okLabel: 'Commit',
      });
      if (msg) runJob({ cmd: 'commit', slug: w.slug, message: msg });
      break;
    }
    case 'merge': {
      const ok = await modal({
        title: 'Merge to main',
        text: `Merge branch maia/${w.slug} back into your base branch? Commit first.`,
        okLabel: 'Merge',
      });
      if (ok) runJob({ cmd: 'merge', slug: w.slug });
      break;
    }
    case 'cleanup': {
      const ok = await modal({
        title: 'Cleanup worktree',
        text: `Remove worktree "${w.slug}" and its branch? Uncommitted changes will be lost.`,
        okLabel: 'Remove',
      });
      if (ok) runJob({ cmd: 'cleanup', slug: w.slug });
      break;
    }
  }
}

async function sendSteer() {
  const w = current();
  const feedback = $('steer-input').value.trim();
  if (!w || !feedback) return;
  $('steer-input').value = '';
  const job = await runJob({ cmd: 'steer', slug: w.slug, feedback });
  if (job) toast('Steering sent.');
}

// ---------- console / SSE ----------
function watchJob(id, switchToConsole) {
  if (switchToConsole && tab !== 'console') switchTab('console');
  if (watching === id && es) { renderConsole(); return; }
  if (es) { es.close(); es = null; }
  watching = id;
  if (!buffers[id]) buffers[id] = [];
  if (!lastIds[id]) lastIds[id] = 0;

  const url = new URL(`/api/jobs/${id}/events`, location.origin);
  url.searchParams.set('after', lastIds[id]);
  if (TOKEN) url.searchParams.set('token', TOKEN);
  es = new EventSource(url.toString());

  es.onmessage = (e) => {
    let ev;
    try { ev = JSON.parse(e.data); } catch (_) { return; }
    if (ev.type === 'end') {
      pushLine(id, 'sys', '── finished ──');
      es.close();
      es = null;
      refresh();
      return;
    }
    if (ev.id && ev.id > lastIds[id]) lastIds[id] = ev.id;
    if (ev.type === 'line') {
      const cls = /error|fail|fatal/i.test(ev.data) ? 'err' : '';
      pushLine(id, cls, ev.data);
    } else if (ev.type === 'status') {
      pushLine(id, 'sys', `── ${ev.data} ──`);
    }
  };
  es.onerror = () => { /* EventSource auto-reconnects */ };
  renderConsole();
}

function pushLine(jobId, cls, text) {
  const buf = buffers[jobId] || (buffers[jobId] = []);
  buf.push({ cls, text });
  if (buf.length > 20000) buf.splice(0, buf.length - 20000);
  if (watching === jobId) renderConsole();
}

function renderConsole() {
  const out = $('console-out');
  const buf = buffers[watching] || [];
  const tail = buf.slice(-1500);
  const atBottom = out.scrollHeight - out.scrollTop - out.clientHeight < 60;
  out.textContent = '';
  for (const line of tail) {
    const span = document.createElement('span');
    span.className = line.cls;
    span.textContent = line.text + '\n';
    out.appendChild(span);
  }
  if (atBottom || tail.length < 50) out.scrollTop = out.scrollHeight;
}

// ---------- wiring ----------
$('btn-new').onclick = async () => {
  const desc = await modal({
    title: 'New change request',
    text: 'Describe what you want to change. A worktree and change.md will be created.',
    value: '',
    placeholder: 'e.g. Add JWT authentication with refresh tokens',
    okLabel: 'Create',
  });
  if (!desc) return;
  const slug = desc.toLowerCase().replace(/[ _]/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '');
  await runJob({ cmd: 'new', description: desc }, { newSlug: slug });
  setTimeout(refresh, 600);
};

document.querySelectorAll('[data-act]').forEach((b) => {
  b.onclick = () => handleAction(b.dataset.act);
});
document.querySelectorAll('.tabs button').forEach((b) => {
  b.onclick = () => switchTab(b.dataset.tab);
});

$('btn-save').onclick = () => saveChange();
$('change-editor').addEventListener('input', () => {
  dirty = true;
  $('save-state').textContent = 'unsaved…';
});
$('change-editor').addEventListener('keydown', (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === 's') { e.preventDefault(); saveChange(); }
  if (e.key === 'Tab') {
    e.preventDefault();
    const el = e.target;
    const { selectionStart: s, selectionEnd: en } = el;
    el.value = el.value.slice(0, s) + '  ' + el.value.slice(en);
    el.selectionStart = el.selectionEnd = s + 2;
    dirty = true;
  }
});

$('btn-steer').onclick = sendSteer;
$('steer-input').addEventListener('keydown', (e) => { if (e.key === 'Enter') sendSteer(); });

$('job-select').onchange = () => watchJob($('job-select').value, false);
$('btn-cancel').onclick = async () => {
  if (!watching) return;
  try { await api(`/api/jobs/${watching}/cancel`, { method: 'POST' }); }
  catch (e) { toast(e.message, true); }
};

window.addEventListener('beforeunload', () => { if (dirty) saveChange(); });

// ---------- boot ----------
refresh();
setInterval(refresh, 3000);
})();
