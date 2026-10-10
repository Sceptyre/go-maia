// screens/dashboard.js — default view: supported repos (projects) grid.

import { get, post, del } from '../api.js';
import { h, clear, toast, modal, relTime, emptyState, statusBadge, spinner } from '../ui.js';

export async function render(root, ctx) {
  clear(root);
  const head = h('div', { class: 'page-head' },
    h('div', {},
      h('h1', {}, 'Projects'),
      h('div', { class: 'muted sub' }, 'Supported repositories you manage change requests for')),
    h('div', { class: 'head-actions' },
      h('button', { class: 'btn btn-primary', onclick: () => openAddProject(ctx) }, '+ Add project'),
      h('button', { class: 'btn', title: 'Settings', onclick: () => { location.hash = '#/settings'; } }, '⚙')));

  const grid = h('div', { class: 'cards' }, h('div', { class: 'loading' }, spinner(), ' Loading projects…'));
  root.append(head, grid);

  let projects;
  try {
    projects = await get('/api/projects');
  } catch (e) {
    clear(grid);
    grid.appendChild(h('div', { class: 'error-box' }, e.message));
    return;
  }

  clear(grid);
  if (!projects.length) {
    grid.appendChild(emptyState(
      'No projects yet',
      'Add a git repository path to start managing change requests with maia.'));
    return;
  }

  for (const p of projects) grid.appendChild(projectCard(p, ctx));
}

function projectCard(p, ctx) {
  const statuses = Object.entries(p.statuses || {})
    .sort((a, b) => b[1] - a[1])
    .map(([s, n]) => h('span', { class: 'chip' }, `${n} ${s}`));

  const gh = p.github && p.github.enabled
    ? h('span', { class: 'chip chip-gh' }, `gh ${p.github.repo || p.github.mode}`)
    : h('span', { class: 'chip muted-chip' }, 'gh off');

  return h('div', {
    class: 'card card-project',
    onclick: () => { location.hash = `#/p/${p.id}`; },
  },
    h('div', { class: 'card-top' },
      h('div', { class: 'card-title' }, p.name),
      h('div', { class: 'spacer' }),
      h('button', {
        class: 'btn btn-tiny btn-ghost', title: 'Project settings',
        onclick: (e) => { e.stopPropagation(); ctx.projectSettings(p); },
      }, '⚙')),
    h('div', { class: 'card-path mono' }, p.path),
    h('div', { class: 'card-row' },
      h('span', { class: 'cr-count' }, `${p.cr_count} CR${p.cr_count === 1 ? '' : 's'}`),
      statuses.slice(0, 4), gh,
      h('span', { class: 'spacer' }),
      h('span', { class: 'muted tiny' }, relTime(p.last_opened))),
  );
}

function addProjectModal(ctx) {
  const path = h('input', { class: 'input', placeholder: '/path/to/repo', spellcheck: 'false' });
  const err = h('div', { class: 'error-text' });
  const add = async () => {
    try {
      const res = await post('/api/projects', { path: path.value.trim() });
      m.close();
      toast(res.created ? `Added ${res.project.name}` : `${res.project.name} already registered`, 'ok');
      window.dispatchEvent(new CustomEvent('maia:projects-changed'));
      ctx.refresh();
    } catch (e) { err.textContent = e.message; }
  };
  path.addEventListener('keydown', (e) => { if (e.key === 'Enter') add(); });
  const m = modal({
    title: 'Add project',
    body: h('div', { class: 'form' },
      h('label', { class: 'muted' }, 'Absolute path to a git repository'),
      path, err),
    actions: [
      h('button', { class: 'btn', onclick: () => m.close() }, 'Cancel'),
      h('button', { class: 'btn btn-primary', onclick: add }, 'Add'),
    ],
  });
  path.focus();
}

// Opened from the sidebar "Add project" row and the command palette.
export { addProjectModal as openAddProject };
