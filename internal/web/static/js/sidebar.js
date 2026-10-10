// sidebar.js — OpenHands-style shell navigation: desktop rail, mobile drawer,
// contextual top bar. One body builder feeds both the rail and the drawer.

import { get } from './api.js';
import { h, clear, spinner } from './ui.js';

const TRANSITION_MS = 250;

const ICON = {
  plus: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>',
  home: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7h-6v7H4a1 1 0 0 1-1-1z"/></svg>',
  search: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/></svg>',
  repo: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>',
  issues: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 8v4M12 16h.01"/></svg>',
  gear: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>',
  chevL: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>',
  chevR: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>',
  x: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>',
};

let els = null;
let ctxRef = null;
let projects = [];          // from /api/projects (cr_count etc.)
let meta = null;            // server info tile (from /api/settings)
let crsCache = new Map();   // pid -> { at, list }
let drawerOpen = false;
let drawerTimer = null;

// --- mobile top bar state (mirrors OpenHands getMobileTopBarState) ---

export function topBarState(hash) {
  const p = (hash || '#/').split('/'); // ['#','p',pid,'cr',slug,tab]
  if (p[1] === 'p' && p[2]) {
    const name = (projects.find((x) => x.id === p[2]) || {}).name || 'project';
    if (p[3] === 'cr' && p[4]) return { to: `#/p/${p[2]}`, label: `Back to ${name}` };
    if (p[3] === 'issues') return { to: `#/p/${p[2]}`, label: `Back to ${name}` };
    return { to: '#/', label: 'Back to Projects' };
  }
  if (p[1] === 'settings' && p[2]) return { to: '#/settings', label: 'Back to Settings' };
  return null;
}

function syncTopBar() {
  if (!els) return;
  const st = topBarState(location.hash || '#/');
  els.back.classList.toggle('hidden', !st);
  if (st) {
    els.back.setAttribute('aria-label', st.label);
    els.back.title = st.label;
    els.back.dataset.to = st.to;
  } else {
    delete els.back.dataset.to;
  }
}

// --- drawer open/close (mount → frame → visible; close reverses) ---

function openDrawer() {
  if (drawerOpen) return;
  drawerOpen = true;
  clearTimeout(drawerTimer);
  els.drawer.classList.remove('hidden');
  els.overlay.classList.remove('hidden');
  els.toggle.setAttribute('aria-expanded', 'true');
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      els.drawer.classList.add('show');
      els.overlay.classList.add('show');
    });
  });
}

function closeDrawer() {
  if (!drawerOpen) return;
  drawerOpen = false;
  els.toggle.setAttribute('aria-expanded', 'false');
  els.drawer.classList.remove('show');
  els.overlay.classList.remove('show');
  drawerTimer = setTimeout(() => {
    els.drawer.classList.add('hidden');
    els.overlay.classList.add('hidden');
  }, TRANSITION_MS);
}

// --- data ---

async function fetchProjects() {
  try {
    projects = await get('/api/projects');
  } catch (e) { /* rail keeps the last list */ }
}

async function fetchMeta() {
  try {
    const s = await get('/api/settings');
    meta = s.server;
  } catch (e) { meta = null; }
}

async function fetchCRs(pid) {
  const hit = crsCache.get(pid);
  if (hit && Date.now() - hit.at < 10000) return hit.list;
  try {
    const list = await get(`/api/projects/${pid}/worktrees`);
    crsCache.set(pid, { at: Date.now(), list });
    return list;
  } catch (e) { return (hit && hit.list) || []; }
}

// --- body builder (shared by rail and drawer) ---

function navRow({ to, icon, label, title, active, indent, onclick }) {
  const row = h('a', {
    class: 'nav-row' + (active ? ' active' : '') + (indent ? ' indent' : ''),
    href: to || '#',
    title: title || label,
  },
    icon ? h('span', { class: 'ico', html: icon }) : null,
    h('span', { class: 'label' }, label));
  if (onclick) row.addEventListener('click', onclick);
  return row;
}

function buildBody(mode) {
  // mode: 'rail' (desktop aside) | 'drawer'
  const hash = location.hash || '#/';
  const p = hash.split('/');
  const onHome = p[1] !== 'p' && p[1] !== 'settings';
  const activePid = p[1] === 'p' ? p[2] : null;

  const collapsed = mode === 'rail' && document.body.classList.contains('sb-collapsed');

  const head = h('div', { class: 'sb-head' },
    h('a', { class: 'brand', href: '#/', title: 'maia' }, collapsed ? 'm' : 'maia'),
    h('span', { class: 'spacer' }),
    mode === 'rail'
      ? h('button', {
        class: 'icon-btn sb-collapse', type: 'button',
        title: collapsed ? 'Expand sidebar' : 'Collapse sidebar',
        'aria-label': collapsed ? 'Expand sidebar' : 'Collapse sidebar',
        html: collapsed ? ICON.chevR : ICON.chevL,
        onclick: () => toggleCollapsed(),
      })
      : h('button', {
        class: 'icon-btn', type: 'button', title: 'Close menu',
        'aria-label': 'Close menu', html: ICON.chevL, onclick: closeDrawer,
      }));

  const cmd = h('button', {
    class: 'cmd-trigger', type: 'button',
    onclick: () => { window.dispatchEvent(new CustomEvent('maia:palette')); },
  },
    h('span', { class: 'ico', html: ICON.search }),
    h('span', { class: 'label' }, 'Search…'),
    h('kbd', { class: 'label' }, 'Ctrl K'));

  const nav = h('nav', { class: 'sb-nav' },
    collapsed ? null : cmd,
    navRow({
      to: '#/', icon: ICON.plus, label: 'Add project', title: 'Add project',
      active: false,
      onclick: (e) => { e.preventDefault(); closeDrawer(); window.dispatchEvent(new CustomEvent('maia:add-project')); },
    }),
    navRow({ to: '#/', icon: ICON.home, label: 'Projects', title: 'Projects', active: onHome }));

  const list = h('div', { class: 'sb-list' });
  if (collapsed) {
    nav.appendChild(navRow({ to: '#/', icon: ICON.search, label: 'Search', title: 'Search', onclick: (e) => { e.preventDefault(); window.dispatchEvent(new CustomEvent('maia:palette')); } }));
  }
  for (const proj of projects) {
    const isActive = proj.id === activePid;
    list.appendChild(navRow({
      to: `#/p/${proj.id}`, icon: ICON.repo, label: proj.name,
      title: `${proj.name} — ${proj.path}`, active: isActive,
    }));
    if (isActive && !collapsed) {
      list.appendChild(navRow({
        to: `#/p/${proj.id}/issues`, icon: ICON.issues, label: 'Issues',
        title: 'GitHub issues', indent: true,
        active: p[3] === 'issues',
      }));
      // change requests of the active project (lazy, cached)
      const crBox = h('div', { class: 'sb-crs' }, h('div', { class: 'sb-crs-loading muted tiny' }, '…'));
      list.appendChild(crBox);
      fetchCRs(proj.id).then((crs) => {
        if (!crBox.isConnected) return;
        clear(crBox);
        for (const cr of crs) {
          crBox.appendChild(navRow({
            to: `#/p/${proj.id}/cr/${cr.slug}/docs`, label: cr.slug,
            title: `${cr.slug} — ${cr.status}`, indent: true,
            active: hash.includes(`/cr/${cr.slug}`),
          }));
        }
      });
    }
  }
  if (!projects.length) {
    list.appendChild(h('div', { class: 'muted tiny sb-empty' }, 'No projects yet.'));
  }

  const foot = h('div', { class: 'sb-foot' });
  if (collapsed && meta) {
    foot.appendChild(h('a', {
      class: 'nav-row', href: '#/settings/server', title: 'Settings',
      'aria-label': 'Settings',
      html: `<span class="ico">${ICON.gear}</span>`,
    }));
  } else if (meta) {
    foot.appendChild(h('a', {
      class: 'sb-server', href: '#/settings/server', title: 'Server settings',
    },
      h('span', { class: 'dot' }),
      h('span', { class: 'label' }, `${meta.version} · ${meta.addr}:${meta.port}`),
      h('span', { class: 'ico', html: ICON.gear })));
  } else {
    foot.appendChild(h('div', { class: 'muted tiny' }, 'server offline?'));
  }

  return h('div', { class: 'sb-body' }, head, nav, list, foot);
}

// --- collapse (desktop, persisted) ---

function toggleCollapsed(force) {
  const next = force !== undefined
    ? force
    : !document.body.classList.contains('sb-collapsed');
  document.body.classList.toggle('sb-collapsed', next);
  try { localStorage.setItem('maia.sb', next ? 'collapsed' : 'open'); } catch (e) { /* private mode */ }
  draw();
}

// --- draw (cheap; no fetches) ---

function draw() {
  if (!els) return;
  clear(els.rail).appendChild(buildBody('rail'));
  clear(els.drawer).appendChild(buildBody('drawer'));
  syncTopBar();
}

// --- public refresh (optionally refetch) ---

export async function refreshSidebar({ fetch: doFetch = false } = {}) {
  if (!els) return;
  if (doFetch) {
    await Promise.all([
      fetchProjects(),
      meta === null ? fetchMeta() : Promise.resolve(),
    ]);
  }
  draw();
}

export function invalidateCRs() { crsCache.clear(); }

// --- init ---

export function initSidebar(ctx) {
  ctxRef = ctx;
  els = {
    rail: document.getElementById('sidebar'),
    drawer: document.getElementById('drawer'),
    overlay: document.getElementById('drawer-overlay'),
    toggle: document.getElementById('navtoggle'),
    back: document.getElementById('backbtn'),
  };

  try {
    if (localStorage.getItem('maia.sb') === 'collapsed') {
      document.body.classList.add('sb-collapsed');
    }
  } catch (e) { /* private mode */ }

  els.toggle.addEventListener('click', () => (drawerOpen ? closeDrawer() : openDrawer()));
  els.overlay.addEventListener('click', closeDrawer);
  els.back.addEventListener('click', () => {
    const to = els.back.dataset.to;
    closeDrawer();
    if (to) location.hash = to;
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && drawerOpen) closeDrawer();
  });
  window.addEventListener('hashchange', () => {
    closeDrawer(); // OH: navigating closes the drawer
    refreshSidebar({ fetch: true });
  });
  window.addEventListener('maia:projects-changed', () => {
    crsCache.clear();
    refreshSidebar({ fetch: true });
  });
  window.addEventListener('maia:toggle-sidebar', () => toggleCollapsed());

  refreshSidebar({ fetch: true });
}
