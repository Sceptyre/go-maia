// screens/issues.js — GitHub issue detection & import (lazy materialization).

import { get, post } from '../api.js';
import { h, clear, toast, relTime, emptyState, statusBadge, spinner } from '../ui.js';

export async function render(root, ctx) {
  const pid = ctx.parts[2];
  clear(root);

  root.appendChild(h('div', { class: 'page-head' },
    h('div', { class: 'crumbs' },
      h('a', { href: `#/p/${pid}`, class: 'back' }, '← Change requests'),
      h('h1', {}, 'GitHub Issues'),
      h('div', { class: 'muted sub' }, 'open issues detected as potential change requests'))));

  const body = h('div', {});
  root.appendChild(body);
  body.appendChild(h('div', { class: 'loading pad' }, spinner(), ' Loading issues…'));

  let data;
  try {
    data = await get(`/api/projects/${pid}/issues`);
  } catch (e) {
    clear(body);
    body.appendChild(h('div', { class: 'error-box' }, e.message));
    return;
  }
  clear(body);

  // auth banner
  if (!data.auth.ok) {
    body.appendChild(h('div', { class: 'error-box' },
      h('div', { class: 'strong' }, 'GitHub not connected'),
      h('div', {}, data.auth.error),
      h('div', { class: 'muted tiny' },
        'Token chain: gh CLI (gh auth login) → GH_TOKEN/GITHUB_TOKEN env → github_token in Settings.')));
    return;
  }

  const cap = (ctx.state.defaults && ctx.state.defaults.max_active_worktrees) || 0;
  const header = h('div', { class: 'issue-toolbar' },
    h('span', { class: 'chip chip-gh' }, data.repo),
    h('span', { class: 'muted tiny' }, `${data.auth.login} via ${data.auth.source}`),
    h('span', { class: 'spacer' }),
    data.last_scan ? h('span', { class: 'muted tiny' }, `scanned ${relTime(data.last_scan)}`) : null,
    h('button', {
      class: 'btn btn-tiny',
      onclick: async (e) => {
        const b = e.currentTarget;
        b.disabled = true;
        b.textContent = 'scanning…';
        try {
          const res = await post(`/api/projects/${pid}/issues/scan`);
          if (res.queued) toast(`Auto-import queued for ${res.queued} issue(s)`, 'ok');
          else toast('Scan complete', 'ok');
          render(root, ctx);
        } catch (er) { toast(er.message, 'err'); b.disabled = false; b.textContent = 'Scan now'; }
      },
    }, 'Scan now'));
  body.appendChild(header);

  if (data.auth.error) {
    body.appendChild(h('div', { class: 'error-box' }, data.auth.error));
    return;
  }

  if (!data.issues.length) {
    body.appendChild(emptyState(
      'No open issues detected',
      'Issues with the configured labels show up here. Tag an issue with "maia" on GitHub, then Scan.'));
    return;
  }

  const capFull = cap > 0 && (ctx.state.projects || [])
    .find((p) => p.id === pid) && ctx.state.projects.find((p) => p.id === pid).cr_count >= cap;

  const list = h('div', { class: 'cards' });
  for (const is of data.issues) {
    list.appendChild(issueCard(pid, is, capFull, ctx));
  }
  body.appendChild(list);
}

function issueCard(pid, is, capFull, ctx) {
  const labels = (is.labels || []).map((l) => h('span', { class: 'chip' }, l.name));
  const right = is.imported_slug
    ? h('button', {
        class: 'btn btn-tiny',
        onclick: () => { location.hash = `#/p/${pid}/cr/${is.imported_slug}`; },
      }, is.imported_status ? ['open · ', statusBadge(is.imported_status)] : 'open')
    : h('button', {
        class: 'btn btn-tiny btn-primary',
        title: capFull ? 'At max_active_worktrees — cleanup a CR first' : 'Materialize a worktree + change.md for this issue',
        onclick: async (e) => {
          const b = e.currentTarget;
          b.disabled = true;
          try {
            await post(`/api/projects/${pid}/issues/${is.number}/import`);
            toast(`Importing #${is.number} — worktree is being created…`, 'ok');
            window.dispatchEvent(new CustomEvent('maia:job-started'));
            const prev = ctx.onJobEnd;
            ctx.onJobEnd = () => { if (prev) prev(); ctx.refresh(); };
          } catch (er) {
            b.disabled = false;
            toast(er.message, 'err', 6000);
          }
        },
        disabled: !!capFull,
      }, capFull ? 'at cap' : 'Import');

  return h('div', { class: 'card card-issue' },
    h('div', { class: 'card-top' },
      h('span', { class: 'issue-num mono' }, '#' + is.number),
      h('div', { class: 'card-title' }, is.title),
      h('span', { class: 'spacer' }),
      right),
    h('div', { class: 'card-row' },
      labels,
      h('span', { class: 'spacer' }),
      h('span', { class: 'muted tiny' }, 'updated ' + relTime(is.updated_at)),
      is.html_url ? h('a', {
        class: 'chip', href: is.html_url, target: '_blank', rel: 'noopener',
        onclick: (e) => e.stopPropagation(),
      }, 'github ↗') : null));
}
