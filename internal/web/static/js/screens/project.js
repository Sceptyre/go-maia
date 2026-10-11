// screens/project.js — change-request list + CR detail (Docs / Diff / Run).

import { get, post, put, del, withToken } from '../api.js';
import {
  h, clear, toast, modal, confirmModal, relTime, emptyState,
  statusBadge, spinner,
} from '../ui.js';
import { consoleEl, startJob, latestJobFor } from '../jobs.js';

export async function render(root, ctx) {
  const [, , pid, , slug, tab] = ctx.parts;
  if (slug) return renderCR(root, ctx, pid, slug, tab || 'docs');
  return renderList(root, ctx, pid);
}

// --- CR list ---

async function renderList(root, ctx, pid) {
  clear(root);
  let project, crs;
  try {
    [project, crs] = await Promise.all([
      get(`/api/projects/${pid}`),
      get(`/api/projects/${pid}/worktrees`),
    ]);
  } catch (e) {
    toast(e.message, 'err');
    location.hash = '#/';
    return;
  }

  const cap = (ctx.state.defaults && ctx.state.defaults.max_active_worktrees) || 0;
  const materialized = project.worktree_count || 0;
  const head = h('div', { class: 'page-head' },
    h('div', { class: 'crumbs' },
      h('a', { href: '#/', class: 'back' }, '← Projects'),
      h('h1', {}, project.name),
      h('div', { class: 'muted mono sub' }, project.path)),
    h('div', { class: 'head-actions' },
      h('button', { class: 'btn', onclick: () => { location.hash = `#/p/${pid}/issues`; } }, 'Issues'),
      h('button', { class: 'btn', title: 'Project settings', onclick: () => ctx.projectSettings(project) }, '⚙'),
      h('button', { class: 'btn btn-primary', onclick: () => newCRModal(ctx, pid) }, '+ New CR')));
  root.appendChild(head);

  const runningJobs = (ctx.state.running_jobs || (ctx.state.running_job ? [ctx.state.running_job] : []))
    .filter((job) => job.project_id === pid);
  for (const running of runningJobs) {
    root.appendChild(h('div', { class: 'notice' },
      `Running: ${running.command}`,
      running.slug ? ` — ${running.slug}` : '',
      h('span', { class: 'spacer' }),
      h('a', {
        class: 'btn btn-tiny',
        href: running.slug ? `#/p/${pid}/cr/${running.slug}/run` : `#/p/${pid}/issues`,
      }, 'open console')));
  }

  // status filter chips
  const counts = {};
  for (const c of crs) counts[c.status] = (counts[c.status] || 0) + 1;
  let filter = 'all';
  const chipRow = h('div', { class: 'chip-row' });
  const list = h('div', { class: 'cards' });

  const drawChips = () => {
    clear(chipRow);
    const mk = (label, val) => h('button', {
      class: 'chip btn-chip' + (filter === val ? ' on' : ''),
      onclick: () => { filter = val; drawChips(); drawList(); },
    }, label);
    chipRow.appendChild(mk(`All ${crs.length}`, 'all'));
    for (const [s, n] of Object.entries(counts).sort()) chipRow.appendChild(mk(`${s} ${n}`, s));
    if (cap > 0) {
      chipRow.appendChild(h('span', { class: 'spacer' }));
      chipRow.appendChild(h('span', { class: 'muted tiny' + (materialized >= cap ? ' warn' : '') },
        `worktrees ${materialized}/${cap}`));
    }
  };

  const drawList = () => {
    clear(list);
    const shown = crs.filter((c) => filter === 'all' || c.status === filter);
    if (!shown.length) {
      list.appendChild(emptyState(
        crs.length ? 'No change requests with this status' : 'No change requests yet',
        crs.length ? 'Pick another filter above.' : 'Create one with + New CR, or import a GitHub issue.'));
      return;
    }
    for (const c of shown) list.appendChild(crCard(c, pid));
  };

  drawChips();
  drawList();
  root.append(chipRow, list);
}

function crCard(c, pid) {
  return h('div', { class: 'card card-cr', onclick: () => { location.hash = `#/p/${pid}/cr/${c.slug}`; } },
    h('div', { class: 'card-top' },
      statusBadge(c.status),
      h('div', { class: 'card-title' }, c.description || c.slug),
      h('span', { class: 'spacer' }),
      c.issue ? h('span', { class: 'chip chip-issue' }, `#${c.issue}`) : null,
      !c.materialized ? h('span', { class: 'chip muted-chip' }, 'no worktree') : null,
      c.rebase_conflict ? h('span', { class: 'chip warn' }, 'rebase conflict') : null,
      c.source_branch ? h('span', { class: 'chip' }, `from ${c.source_branch}`) : null,
      c.pr ? h('span', { class: 'chip chip-pr' }, 'PR ↗') : null,
      h('span', { class: 'chev' }, '›')),
    h('div', { class: 'mono tiny muted' }, c.slug));
}

async function newCRModal(ctx, pid) {
  let branchData;
  try { branchData = await get(`/api/projects/${pid}/branches`); }
  catch (e) { toast(e.message, 'err'); return; }
  const source = h('select', { class: 'input' },
    ...branchData.branches.map((branch) => h('option', { value: branch }, branch)));
  source.value = branchData.default_branch;
  const ta = h('textarea', { class: 'input', rows: 3, placeholder: 'Describe the change you want…' });
  const err = h('div', { class: 'error-text' });
  const go = async () => {
    try {
      const cr = await post(`/api/projects/${pid}/change-requests`, {
        description: ta.value.trim(), base: source.value,
      });
      m.close();
      window.dispatchEvent(new CustomEvent('maia:projects-changed'));
      try {
        await post(`/api/projects/${pid}/worktrees/${cr.slug}/materialize`);
        toast('Creating change request worktree…', 'ok');
        await ctx.refreshState();
        location.hash = `#/p/${pid}/cr/${cr.slug}/run`;
      } catch (e) {
        await ctx.refreshState();
        toast(e.code === 409 ? 'Change request saved; materialize it when a worktree slot is free.' : e.message, e.code === 409 ? 'info' : 'err', 6000);
        location.hash = `#/p/${pid}/cr/${cr.slug}/docs`;
      }
    } catch (e) { err.textContent = e.message; }
  };
  const m = modal({
    title: 'New change request',
    body: h('div', { class: 'form' },
      h('label', { class: 'muted' }, 'Source branch — new CR branches from this branch'),
      source,
      h('label', { class: 'muted' }, 'Description — the worktree slug is derived from it'),
      ta, err),
    actions: [
      h('button', { class: 'btn', onclick: () => m.close() }, 'Cancel'),
      h('button', { class: 'btn btn-primary', onclick: go }, 'Create'),
    ],
  });
  ta.focus();
}

// --- CR detail ---

async function renderCR(root, ctx, pid, slug, tab) {
  clear(root);
  let crs;
  try {
    crs = await get(`/api/projects/${pid}/worktrees`);
  } catch (e) {
    toast(e.message, 'err');
    location.hash = `#/p/${pid}`;
    return;
  }
  const cr = crs.find((c) => c.slug === slug);
  const running = (ctx.state.running_jobs || (ctx.state.running_job ? [ctx.state.running_job] : []))
    .find((job) => job.project_id === pid && job.slug === slug);
  const creating = !cr && running && running.kind === 'new';

  if (!cr && !creating) {
    toast(`Change request not found: ${slug}`, 'err');
    location.hash = `#/p/${pid}`;
    return;
  }

  const head = h('div', { class: 'page-head' },
    h('div', { class: 'crumbs' },
      h('a', { href: `#/p/${pid}`, class: 'back' }, '← ' + (ctx.projectName || 'project')),
      cr ? h('h1', {}, cr.description || cr.slug) : h('h1', {}, 'Creating…'),
      cr ? h('span', { class: 'mono tiny muted' }, cr.slug) : null,
      cr && cr.source_branch ? h('span', { class: 'chip' }, `from ${cr.source_branch}`) : null,
      cr && cr.rebase_conflict ? h('span', { class: 'chip warn' }, 'rebase conflict') : null),
    h('div', { class: 'head-actions' },
      cr ? statusBadge(cr.status) : spinner(),
      cr && !cr.materialized ? h('button', {
        class: 'btn btn-primary', disabled: !!running,
        onclick: async () => {
          try {
            await post(`/api/projects/${pid}/worktrees/${slug}/materialize`);
            toast('Materializing change request…', 'ok');
            await ctx.refreshState();
            location.hash = `#/p/${pid}/cr/${slug}/run`;
          } catch (e) { toast(e.message, 'err'); }
        },
      }, running ? 'Materializing…' : 'Materialize worktree') : null,
      cr && !cr.materialized ? h('button', {
        class: 'btn btn-danger', disabled: !!running,
        onclick: async () => {
          if (!(await confirmModal(`Delete change request "${cr.description || slug}"? This cannot be undone.`))) return;
          try {
            await del(`/api/projects/${pid}/change-requests/${slug}`);
            window.dispatchEvent(new CustomEvent('maia:projects-changed'));
            location.hash = `#/p/${pid}`;
          } catch (e) { toast(e.message, 'err'); }
        },
      }, 'Delete CR') : null));

  if (cr && cr.issue) {
    head.querySelector('.crumbs').appendChild(h('button', {
      class: 'chip chip-issue',
      title: 'Linked GitHub issue — open the Issues screen',
      onclick: () => { location.hash = `#/p/${pid}/issues`; },
    }, `issue #${cr.issue}`));
  }

  root.appendChild(head);

  if (creating) {
    root.appendChild(h('div', { class: 'loading pad' }, spinner(), ` Creating worktree for "${slug}" — follow the console…`));
    if (running) {
      const cons = consoleEl();
      root.appendChild(cons.el);
      cons.mount(running.id, { onEnd: () => ctx.refresh() });
    }
    return;
  }

  // tab bar
  const bar = h('div', { class: 'tabs' });
  for (const t of ['docs', 'diff', 'run']) {
    bar.appendChild(h('button', {
      class: 'tab' + (tab === t ? ' on' : ''),
      onclick: () => { location.hash = `#/p/${pid}/cr/${slug}/${t}`; },
    }, t[0].toUpperCase() + t.slice(1)));
  }
  root.appendChild(bar);

  const body = h('div', { class: 'tab-body' });
  root.appendChild(body);

  if (tab === 'docs') renderDocs(body, pid, cr);
  else if (tab === 'diff') renderDiff(body, pid, cr);
  else renderRun(body, ctx, pid, cr);
}

// --- docs ---

async function renderDocs(root, pid, cr) {
  let doc = 'change';
  const sub = h('div', { class: 'subtabs' });
  const body = h('div', { class: 'doc-area' });
  root.append(sub, body);

  const drawSub = () => {
    clear(sub);
    for (const d of ['change', 'research', 'plan']) {
      sub.appendChild(h('button', {
        class: 'subtab' + (doc === d ? ' on' : ''),
        onclick: () => { doc = d; drawSub(); draw(); },
      }, d + '.md'));
    }
    if (cr && cr.pr) {
      sub.appendChild(h('span', { class: 'spacer' }));
      sub.appendChild(h('a', { class: 'chip chip-pr', href: cr.pr, target: '_blank', rel: 'noopener' }, 'draft PR ↗'));
    }
  };

  const draw = async () => {
    clear(body);
    body.appendChild(h('div', { class: 'loading' }, spinner()));
    let d;
    try {
      d = await get(`/api/projects/${pid}/worktrees/${cr.slug}/doc/${doc}`);
    } catch (e) {
      clear(body);
      body.appendChild(emptyState(
        `${doc}.md not found`,
        doc === 'change' ? '' : 'Generated docs appear after the corresponding maia phase runs (Init/Plan).'));
      return;
    }
    clear(body);
    if (doc === 'change') drawEditor(body, pid, cr, d.raw);
    else {
      const view = h('div', { class: 'markdown' });
      view.innerHTML = d.html; // server-rendered markdown (goldmark, escaped)
      body.appendChild(view);
    }
  };

  drawSub();
  draw();
}

function drawEditor(root, pid, cr, raw) {
  const ta = h('textarea', { class: 'editor mono', spellcheck: 'false' });
  ta.value = raw;
  const save = h('button', { class: 'btn btn-primary btn-tiny', disabled: true }, 'Save');
  const dirty = h('span', { class: 'muted tiny' }, '');
  ta.addEventListener('input', () => {
    const changed = ta.value !== raw;
    save.disabled = !changed;
    dirty.textContent = changed ? 'unsaved changes' : '';
  });
  save.addEventListener('click', async () => {
    try {
      await put(`/api/projects/${pid}/worktrees/${cr.slug}/doc/change`, { raw: ta.value });
      raw = ta.value;
      save.disabled = true;
      dirty.textContent = '';
      toast('change.md saved', 'ok');
      window.dispatchEvent(new CustomEvent('maia:docs-saved'));
    } catch (e) { toast(e.message, 'err'); }
  });
  root.append(
    h('div', { class: 'editor-bar' },
      h('span', { class: 'muted tiny' }, 'edit frontmatter (status, description) and content directly'),
      h('span', { class: 'spacer' }), dirty, save),
    ta);
}

// --- diff ---

async function renderDiff(root, pid, cr) {
  clear(root);
  if (!cr.materialized) {
    root.appendChild(emptyState('No worktree materialized', 'Materialize this change request to view its diff.'));
    return;
  }
  const bar = h('div', { class: 'editor-bar' },
    h('span', { class: 'muted' }, 'git status'),
    h('span', { class: 'spacer' }),
    h('button', { class: 'btn btn-tiny', onclick: () => draw() }, '↻ refresh'));
  const body = h('div', {});
  root.append(bar, body);

  const draw = async () => {
    clear(body);
    body.appendChild(h('div', { class: 'loading' }, spinner()));
    let d;
    try {
      d = await get(`/api/projects/${pid}/worktrees/${cr.slug}/diff`);
    } catch (e) {
      clear(body);
      body.appendChild(h('div', { class: 'error-box' }, e.message));
      return;
    }
    clear(body);
    if (!d.unstaged && !d.staged && !d.untracked.length) {
      body.appendChild(emptyState('Working tree clean', 'No uncommitted changes in this worktree.'));
      return;
    }
    if (d.untracked.length) {
      body.appendChild(h('div', { class: 'diff-file' }, 'Untracked files'));
      body.appendChild(h('div', { class: 'untracked' }, d.untracked.join('\n')));
    }
    if (d.staged) body.appendChild(diffBlock('Staged changes', d.staged));
    if (d.unstaged) body.appendChild(diffBlock('Unstaged changes', d.unstaged));
  };
  draw();
}

function diffBlock(title, text) {
  const pre = h('pre', { class: 'diff mono' });
  for (const line of text.split('\n')) {
    let cls = '';
    if (line.startsWith('+++') || line.startsWith('---')) cls = 'd-file';
    else if (line.startsWith('@@')) cls = 'd-hunk';
    else if (line.startsWith('+')) cls = 'd-add';
    else if (line.startsWith('-')) cls = 'd-del';
    else if (line.startsWith('diff ') || line.startsWith('index ')) cls = 'd-meta';
    pre.appendChild(h('span', { class: cls || undefined }, line + '\n'));
  }
  return h('div', {}, h('div', { class: 'diff-file' }, title), pre);
}

// --- run ---

function renderRun(root, ctx, pid, cr) {
  const cons = consoleEl();
  const runningJobs = ctx.state.running_jobs || (ctx.state.running_job ? [ctx.state.running_job] : []);
  const running = runningJobs.find((job) => job.project_id === pid && job.slug === cr.slug);
  const others = runningJobs.filter((job) => job.project_id !== pid || job.slug !== cr.slug);

  if (running) root.appendChild(h('div', { class: 'notice' }, `Running: ${running.command} — output below.`));
  else if (others.length) root.appendChild(h('div', { class: 'notice notice-dim' },
    `${others.length} command${others.length === 1 ? '' : 's'} running for other change requests; you can keep working here.`));

	if (cr.rebase_conflict) {
		root.appendChild(h('div', { class: 'notice warn' },
			'Rebase conflict: resolve files in this CR worktree, then Continue rebase or Abort rebase.'));
	}

  if (!cr.materialized) {
    root.appendChild(emptyState('No worktree materialized', 'Materialize this change request before running maia commands.'));
    if (running && running.kind === 'materialize') {
      root.appendChild(cons.el);
      cons.mount(running.id, { onEnd: () => ctx.refresh() });
    }
    return;
  }

  // action button: mkBody returns the request body, or null to abort.
  const action = (label, mkBody, opts = {}) => h('button', {
    class: 'btn' + (opts.primary ? ' btn-primary' : '') + (opts.danger ? ' btn-danger' : ''),
    disabled: !!running || (!!cr.rebase_conflict && !opts.allowDuringConflict),
    onclick: async () => {
      if (opts.confirmMsg && !(await confirmModal(opts.confirmMsg, { okLabel: label }))) return;
      const body = mkBody();
      if (!body) return;
      try {
        const job = await startJob(pid, { slug: cr.slug, ...body });
        cons.mount(job.id, { onEnd: () => ctx.refresh() });
        window.dispatchEvent(new CustomEvent('maia:job-started'));
      } catch (e) { toast(e.message, 'err', 6000); }
    },
  }, label);

  const groups = h('div', { class: 'run-groups' });

  groups.append(
    h('div', { class: 'run-group' },
      h('div', { class: 'run-group-title' }, '1 · Understand'),
      h('div', { class: 'btn-row' }, action('Init', () => ({ cmd: 'init' }), { primary: true }))),

    h('div', { class: 'run-group' },
      h('div', { class: 'run-group-title' }, '2 · Design'),
      h('div', { class: 'btn-row' }, action('Plan', () => ({ cmd: 'plan' }), { primary: true }))));

  // Phase 3: steer + apply
  const feedback = h('textarea', { class: 'input', rows: 2, placeholder: 'what to adjust…' });
  const dryRun = h('input', { type: 'checkbox' });
  const phaseNum = h('input', { class: 'input input-num', type: 'number', min: '1', placeholder: 'all' });
  groups.appendChild(h('div', { class: 'run-group' },
    h('div', { class: 'run-group-title' }, '3 · Build'),
    h('div', { class: 'steer-row' },
      feedback,
      action('Steer', () => {
        const f = feedback.value.trim();
        if (!f) { toast('Feedback is required', 'err'); return null; }
        return { cmd: 'steer', feedback: f };
      }),
    ),
    h('div', { class: 'btn-row' },
      h('label', { class: 'inline-label' }, dryRun, ' dry run'),
      h('label', { class: 'inline-label' }, phaseNum, ' phase'),
      action('Apply', () => ({
        cmd: 'apply',
        dry_run: dryRun.checked,
        phase: phaseNum.value ? Number(phaseNum.value) : 0,
      })))));

  const updateBaseAction = cr.rebase_conflict
    ? action('Continue rebase', () => ({ cmd: 'update-base', continue: true }), { primary: true, allowDuringConflict: true })
    : action('Update from source', () => ({ cmd: 'update-base' }));
  const abortRebaseAction = cr.rebase_conflict
    ? action('Abort rebase', () => ({ cmd: 'update-base', abort: true }), { danger: true, allowDuringConflict: true })
    : null;


  // Phase 4: commit / merge / cleanup / draft PR
  const commitMsg = h('input', { class: 'input grow', placeholder: 'commit message' });
  groups.appendChild(h('div', { class: 'run-group' },
    h('div', { class: 'run-group-title' }, '4 · Ship'),
    h('div', { class: 'steer-row' },
      commitMsg,

      action('Commit', () => {
        const m = commitMsg.value.trim();
        if (!m) { toast('Commit message is required', 'err'); return null; }
        return { cmd: 'commit', message: m };
      })),
    h('div', { class: 'btn-row' },
      updateBaseAction,
      abortRebaseAction,
      action('Merge', () => ({ cmd: 'merge' }), {
        danger: true,
        confirmMsg: 'Merge this change request into the base branch?',
      }),
      action('Cleanup', () => ({ cmd: 'cleanup' }), {
        danger: true,
        confirmMsg: 'Detach this worktree? The change request and branch will be kept and can be materialized again.'
      }),
      prAction(ctx, pid, cr, running))));

  root.appendChild(groups);
  root.appendChild(h('div', { class: 'console-wrap' }, cons.el));

  // Attach existing/latest job output (SSE replays finished jobs from 0).
  (async () => {
    try {
      if (running && running.project_id === pid && running.slug === cr.slug) {
        cons.mount(running.id, { onEnd: () => ctx.refresh() });
      } else {
        const latest = await latestJobFor(pid, cr.slug).catch(() => null);
        if (latest) {
          const finished = latest.status !== 'running';
          cons.mount(latest.id, finished ? {} : { onEnd: () => ctx.refresh() });
        }
      }
    } catch (e) { /* console attach is best-effort */ }
  })();
}

function prAction(ctx, pid, cr, running) {
  return h('button', {
    class: 'btn',
    disabled: !!running,
    onclick: () => {
      const title = h('input', { class: 'input', value: `maia: ${cr.description || cr.slug}` });
      const body = h('textarea', { class: 'input', rows: 4 });
      body.value = cr.issue ? `Closes #${cr.issue}` : '';
      const err = h('div', { class: 'error-text' });
      const go = async () => {
        try {
          const res = await post(`/api/projects/${pid}/pull-requests`, {
            slug: cr.slug, title: title.value.trim(), body: body.value.trim(),
          });
          m.close();
          toast(`Draft PR #${res.pr.number} created`, 'ok', 6000);
          ctx.refresh();
        } catch (e) { err.textContent = e.message; }
      };
      const m = modal({
        title: 'Open draft pull request',
        body: h('div', { class: 'form' },
          h('label', { class: 'muted' }, 'Title'), title,
          h('label', { class: 'muted' }, 'Description'), body, err),
        actions: [
          h('button', { class: 'btn', onclick: () => m.close() }, 'Cancel'),
          h('button', { class: 'btn btn-primary', onclick: go }, 'Create draft PR'),
        ],
      });
    },
  }, 'Draft PR');
}
