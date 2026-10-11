// app.js — bootstrap, router, state polling, running-job banner, PWA setup.

import { state as fetchState, setToken, put, del, get } from './api.js';
import { h, clear, toast, modal, confirmModal, spinner } from './ui.js';
import { closeAllConsoles } from './jobs.js';
import { initSidebar, refreshSidebar } from './sidebar.js';
import { initPalette } from './palette.js';
import * as dashboard from './screens/dashboard.js';
import * as project from './screens/project.js';
import * as issues from './screens/issues.js';
import * as settings from './screens/settings.js';

const view = document.getElementById('view');
const banner = document.getElementById('banner');

const S = { state: null, onJobEnd: null };
let rendering = false, renderQueued = false;

const ctx = {
  get state() { return S.state || { projects: [], defaults: {}, running_job: null, running_jobs: [] }; },
  get onJobEnd() { return S.onJobEnd; },
  set onJobEnd(fn) { S.onJobEnd = fn; },
  refresh: () => { renderQueued = true; maybeRender(); },
  refreshState: () => poll(),
  projectName: '',
  projectSettings,
};

async function maybeRender() {
  if (rendering) return;
  rendering = true;
  try {
    while (renderQueued) {
      renderQueued = false;
      await render();
    }
  } catch (e) {
    // never wedge the render loop: surface and keep going
    renderQueued = false;
    toast('render error: ' + (e && e.message ? e.message : e), 'err', 8000);
  } finally {
    rendering = false;
  }
  if (renderQueued) maybeRender();
}

async function render() {
  closeAllConsoles();
  const hash = location.hash || '#/';
  const parts = hash.split('/'); // ['#', 'p', pid, ...]
  const root = view;

  // best-effort project name for breadcrumbs
  ctx.projectName = '';
  if (parts[1] === 'p' && S.state && S.state.projects) {
    const p = S.state.projects.find((x) => x.id === parts[2]);
    if (p) ctx.projectName = p.name;
  }

  if (parts[1] === 'p' && parts[3] === 'issues') {
    document.title = 'Issues · maia';
    return issues.render(root, { ...ctx, parts });
  }
  if (parts[1] === 'p') {
    document.title = (ctx.projectName || 'project') + ' · maia';
    return project.render(root, { ...ctx, parts });
  }
  if (parts[1] === 'settings') {
    document.title = 'Settings · maia';
    return settings.render(root, { ...ctx, parts }, parts[2]);
  }
  document.title = 'maia';
  return dashboard.render(root, { ...ctx, parts });
}

// --- state polling / running-job banner ---

async function poll() {
  let st;
  try {
    st = await fetchState();
  } catch (e) {
    if (e.code === 401) { tokenGate(); return; }
    if (e.code === 0) {
      banner.innerHTML = '';
      banner.className = 'banner banner-offline';
      banner.textContent = 'server unreachable — retrying…';
      return;
    }
    return;
  }
  const prev = S.state;
  S.state = st;

  drawBanner(st);
  refreshSidebar();

  const wasRunning = prev ? (prev.running_jobs || (prev.running_job ? [prev.running_job] : [])) : [];
  const isRunning = st.running_jobs || (st.running_job ? [st.running_job] : []);
  const before = new Set(wasRunning.map((job) => job.id));
  const started = isRunning.some((job) => !before.has(job.id));
  if (wasRunning.length && !isRunning.length && S.onJobEnd) {
    const fn = S.onJobEnd;
    S.onJobEnd = null;
    try { fn(); } catch (e) { /* screen changed */ }
  }
  if (started) window.dispatchEvent(new CustomEvent('maia:job-started'));
}

function drawBanner(st) {
  const jobs = st.running_jobs || (st.running_job ? [st.running_job] : []);
  if (!jobs.length) { banner.className = 'banner hidden'; clear(banner); return; }
  clear(banner);
  banner.className = 'banner';
  banner.append(spinner(), h('span', {}, `running ${jobs.length} command${jobs.length === 1 ? '' : 's'}:`));
  for (const job of jobs) {
    const href = job.project_id
      ? job.slug ? `#/p/${job.project_id}/cr/${job.slug}/run` : `#/p/${job.project_id}/issues`
      : '#/';
    banner.append(h('a', { class: 'btn btn-tiny', href }, `${job.command}${job.slug ? ` · ${job.slug}` : ''}`));
  }
}

async function tokenGate() {
  const input = h('input', { class: 'input', type: 'password', placeholder: 'API token' });
  const err = h('div', { class: 'error-text' });
  const submit = async () => {
    setToken(input.value.trim());
    const st = await fetchState().catch((e) => e);
    if (st && st.error) { err.textContent = 'invalid token'; return; }
    m.close();
    toast('Token accepted', 'ok');
    await poll();
    render();
  };
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') submit(); });
  const m = modal({
    title: 'maia token required',
    body: h('div', { class: 'form' },
      h('div', { class: 'muted' }, 'This server requires the token printed by `maia web`.'),
      input, err),
    actions: [h('button', { class: 'btn btn-primary', onclick: submit }, 'Connect')],
  });
  input.focus();
}

// --- project settings modal (used by dashboard + project screens) ---

function projectSettings(p) {
  const name = h('input', { class: 'input', value: p.name });
  const base = h('input', { class: 'input', value: p.base_branch || '', placeholder: 'default branch' });
  const ghEnabled = h('input', { type: 'checkbox' });
  ghEnabled.checked = !!(p.github && p.github.enabled);
  const ghMode = h('select', { class: 'input' },
    h('option', { value: 'origin' }, 'origin remote (auto-detect repo)'),
    h('option', { value: 'custom' }, 'custom (owner/repo)'));
  ghMode.value = (p.github && p.github.mode) || 'origin';
  const ghRepo = h('input', { class: 'input', value: (p.github && p.github.repo) || '', placeholder: 'owner/repo' });
  const ghLabels = h('input', { class: 'input', value: ((p.github && p.github.labels) || []).join(', '), placeholder: 'maia' });
  const ghImport = h('select', { class: 'input' },
    h('option', { value: 'manual' }, 'manual'),
    h('option', { value: 'auto' }, 'auto (up to cap)'));
  ghImport.value = (p.github && p.github.import_mode) || 'manual';
  const err = h('div', { class: 'error-text' });

  const save = async () => {
    try {
      await put(`/api/projects/${p.id}`, {
        name: name.value.trim(),
        base_branch: base.value.trim(),
        github: {
          enabled: ghEnabled.checked,
          mode: ghMode.value,
          repo: ghRepo.value.trim(),
          labels: ghLabels.value.split(',').map((s) => s.trim()).filter(Boolean),
          import_mode: ghImport.value,
        },
      });
      m.close();
      toast('Project updated', 'ok');
      window.dispatchEvent(new CustomEvent('maia:projects-changed'));
      ctx.refresh();
      poll();
    } catch (e) { err.textContent = e.message; }
  };

  const remove = async () => {
    if (!(await confirmModal(`Remove "${p.name}" from maia? Worktrees on disk are kept.`, { okLabel: 'Remove' }))) return;
    try {
      await del(`/api/projects/${p.id}`);
      m.close();
      toast('Project removed', 'ok');
      window.dispatchEvent(new CustomEvent('maia:projects-changed'));
      if ((location.hash || '').includes(p.id)) location.hash = '#/';
      else ctx.refresh();
      poll();
    } catch (e) { err.textContent = e.message; }
  };

  const m = modal({
    title: `Project settings — ${p.name}`,
    wide: true,
    body: h('div', { class: 'form' },
      h('label', { class: 'muted tiny' }, 'Display name'), name,
      h('label', { class: 'muted tiny' }, 'Default source branch (blank = detect origin default)'), base,
      h('div', { class: 'form-sep' }, 'GitHub'),
      h('label', { class: 'inline-label' }, ghEnabled, ' enable issue detection & draft PRs'),
      h('label', { class: 'muted tiny' }, 'Repo resolution'), ghMode,
      h('label', { class: 'muted tiny' }, 'Repository (custom mode)'), ghRepo,
      h('label', { class: 'muted tiny' }, 'Detection labels (empty = all issues)'), ghLabels,
      h('label', { class: 'muted tiny' }, 'Import mode'), ghImport,
      err),
    actions: [
      h('button', { class: 'btn btn-danger', onclick: remove }, 'Remove project'),
      h('span', { class: 'spacer' }),
      h('button', { class: 'btn', onclick: () => m.close() }, 'Cancel'),
      h('button', { class: 'btn btn-primary', onclick: save }, 'Save'),
    ],
  });
}

// --- boot ---

window.addEventListener('hashchange', () => { renderQueued = true; maybeRender(); });
document.addEventListener('visibilitychange', () => { if (!document.hidden) poll(); });

// surface silent exceptions instead of leaving the UI half-wired
window.addEventListener('error', (e) => toast('js error: ' + e.message, 'err', 8000));
window.addEventListener('unhandledrejection', (e) => {
  const r = e.reason;
  toast('promise error: ' + (r && r.message ? r.message : String(r)), 'err', 8000);
});

if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch(() => { /* offline file:// etc. */ });
}

// shell navigation events (sidebar + palette ask the owner screens to act)
window.addEventListener('maia:add-project', () => dashboard.openAddProject(ctx));

(async function boot() {
  const st = await fetchState().catch((e) => e);
  if (st && st.error && st.error.includes('token')) { await tokenGate(); }
  else if (st && !st.error) S.state = st;
  drawBanner(S.state || {});
  initSidebar(ctx);
  initPalette(ctx);
  renderQueued = true;
  await maybeRender();
  await poll();
  setInterval(() => { if (!document.hidden) poll(); }, 3000);
})();
