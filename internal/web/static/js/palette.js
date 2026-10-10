// palette.js — ⌘K command menu, following the OpenHands command-menu flow:
// grouped (navigation / settings / actions), filter-as-you-type, Enter performs.

import { h, clear } from './ui.js';

const GROUPS = ['navigation', 'settings', 'actions'];
const GROUP_LABELS = { navigation: 'Go to', settings: 'Settings', actions: 'Actions' };

let ctxRef = null;
let root = null;
let ov = null, input = null, listEl = null;
let visible = [], sel = 0;

function icon(svg) {
  const s = h('span', { class: 'ico' });
  s.innerHTML = svg;
  return s;
}

const ICO = {
  home: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7h-6v7H4a1 1 0 0 1-1-1z"/></svg>',
  repo: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>',
  issues: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 8v4M12 16h.01"/></svg>',
  gear: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>',
  plus: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>',
  panel: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/></svg>',
};

// buildItems returns the command list for the current route + state.
function buildItems() {
  const hash = location.hash || '#/';
  const p = hash.split('/');
  const state = ctxRef ? ctxRef.state : { projects: [] };
  const projects = state.projects || [];
  const items = [];

  items.push({
    group: 'navigation', icon: ICO.home, title: 'Projects',
    desc: 'all repositories', kw: 'home dashboard repos', to: '#/',
  });
  for (const proj of projects) {
    items.push({
      group: 'navigation', icon: ICO.repo, title: proj.name,
      desc: proj.path, kw: 'project repo ' + proj.name, to: `#/p/${proj.id}`,
    });
    if (p[1] === 'p' && p[2] === proj.id) {
      items.push({
        group: 'navigation', icon: ICO.issues, title: `Issues — ${proj.name}`,
        desc: 'github issues labeled maia', kw: 'issues github', to: `#/p/${proj.id}/issues`,
      });
    }
  }

  items.push(
    { group: 'settings', icon: ICO.gear, title: 'Settings', desc: 'overview', kw: 'preferences', to: '#/settings' },
    { group: 'settings', icon: ICO.gear, title: 'Server', desc: 'version, address, token', kw: 'server host port', to: '#/settings/server' },
    { group: 'settings', icon: ICO.gear, title: 'Web defaults', desc: 'worktree cap, detection, import', kw: 'defaults cap labels poll', to: '#/settings/defaults' },
    { group: 'settings', icon: ICO.gear, title: 'maia config', desc: 'model, API keys', kw: 'config llm api key model', to: '#/settings/config' },
  );

  items.push(
    {
      group: 'actions', icon: ICO.plus, title: 'Add project',
      desc: 'register a git repository', kw: 'add new project repository',
      perform: () => window.dispatchEvent(new CustomEvent('maia:add-project')),
    },
    {
      group: 'actions', icon: ICO.panel,
      title: document.body.classList.contains('sb-collapsed') ? 'Expand sidebar' : 'Collapse sidebar',
      desc: 'toggle navigation rail', kw: 'sidebar collapse expand toggle',
      perform: () => window.dispatchEvent(new CustomEvent('maia:toggle-sidebar')),
    },
  );
  return items;
}

function drawList() {
  clear(listEl);
  const q = input.value.trim().toLowerCase();
  visible = buildItems().filter((it) => {
    if (!q) return true;
    return (it.title + ' ' + (it.desc || '') + ' ' + (it.kw || '')).toLowerCase().includes(q);
  });
  sel = Math.min(sel, Math.max(visible.length - 1, 0));

  let idx = -1;
  for (const g of GROUPS) {
    const group = visible.filter((it) => it.group === g);
    if (!group.length) continue;
    listEl.appendChild(h('div', { class: 'palette-group-label' }, GROUP_LABELS[g]));
    for (const it of group) {
      idx += 1;
      const row = h('div', {
        class: 'palette-item' + (idx === sel ? ' sel' : ''),
        role: 'option',
      },
        icon(it.icon),
        h('span', { class: 'pi-title' }, it.title),
        it.desc ? h('span', { class: 'pi-desc' }, it.desc) : null);
      row.addEventListener('click', () => run(it));
      row.addEventListener('mousemove', () => {
        if (sel !== idx) { sel = idx; drawList(); }
      });
      listEl.appendChild(row);
    }
  }
  if (!visible.length) listEl.appendChild(h('div', { class: 'palette-empty muted' }, 'No matches'));
}

function run(it) {
  close();
  if (it.to) location.hash = it.to;
  else if (it.perform) it.perform();
}

function open() {
  if (ov) return;
  input.value = '';
  sel = 0;
  ov = h('div', { class: 'palette-ov' },
    h('div', { class: 'palette', role: 'dialog', 'aria-label': 'Command menu' },
      input,
      h('div', { class: 'palette-groups', role: 'listbox' }, listEl)));
  ov.addEventListener('mousedown', (e) => { if (e.target === ov) close(); });
  root.appendChild(ov);
  drawList();
  input.focus();
}

function close() {
  if (!ov) return;
  ov.remove();
  ov = null;
}

export function initPalette(ctx) {
  ctxRef = ctx;
  root = document.getElementById('palette-root');
  input = h('input', {
    class: 'palette-input', type: 'text', spellcheck: 'false',
    placeholder: 'Search projects, settings, actions…',
    'aria-label': 'Command menu',
  });
  listEl = h('div', { class: 'palette-list' });
  input.addEventListener('input', () => { sel = 0; drawList(); });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); sel = Math.min(sel + 1, visible.length - 1); drawList(); scrollSel(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); sel = Math.max(sel - 1, 0); drawList(); scrollSel(); }
    else if (e.key === 'Enter') { e.preventDefault(); if (visible[sel]) run(visible[sel]); }
    else if (e.key === 'Escape') { e.preventDefault(); close(); }
  });

  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
      e.preventDefault();
      ov ? close() : open();
    } else if (e.key === 'Escape' && ov) {
      close();
    }
  });
  window.addEventListener('maia:palette', () => (ov ? close() : open()));
}

function scrollSel() {
  const el = listEl.querySelector('.palette-item.sel');
  if (el) el.scrollIntoView({ block: 'nearest' });
}
