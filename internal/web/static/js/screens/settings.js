// screens/settings.js — admin dashboard following the OpenHands settings flow:
// #/settings is the hub (mobile: section list; desktop: redirects to the first
// section), and each section lives at its own route with a section nav.

import { get, put } from '../api.js';
import { h, clear, toast, spinner } from '../ui.js';

const SECTIONS = [
  { id: 'server', title: 'Server', sub: 'version, address, and token status', icon: 'host' },
  { id: 'defaults', title: 'Web defaults', sub: 'worktree cap, issue detection, import mode', icon: 'sliders' },
  { id: 'config', title: 'maia config', sub: 'model, API keys, and tokens', icon: 'key' },
];

const HUB_ICONS = {
  host: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><path d="M6 7h.01M6 17h.01"/></svg>',
  sliders: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6"/></svg>',
  key: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="15" r="4"/><path d="m10.85 12.15 7.15-7.15M18 5l2 2M15 8l2 2"/></svg>',
  chev: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>',
};

export async function render(root, ctx, sectionId) {
  clear(root);

  // Desktop hub → redirect to the first section (OpenHands behavior);
  // mobile keeps the hub as the full-screen section list.
  if (!sectionId && window.matchMedia('(min-width: 700px)').matches) {
    location.replace('#/settings/server');
    return;
  }

  if (!sectionId) return renderHub(root);

  const meta = SECTIONS.find((s) => s.id === sectionId);
  if (!meta) { location.replace('#/settings'); return; }

  const nav = h('nav', { class: 'settings-nav', 'aria-label': 'Settings sections' });
  for (const s of SECTIONS) {
    nav.appendChild(h('a', {
      class: 'nav-row' + (s.id === sectionId ? ' active' : ''),
      href: `#/settings/${s.id}`, title: s.title,
    },
      h('span', { class: 'ico', html: HUB_ICONS[s.icon] }),
      h('span', { class: 'label' }, s.title)));
  }

  const body = h('div', { class: 'settings-body' });
  const content = h('div', { class: 'settings-content' },
    h('header', { class: 'settings-head' },
      h('h2', {}, meta.title),
      h('p', { class: 'muted sub' }, meta.sub)),
    body);

  const layout = h('div', { class: 'settings-layout' }, nav, content);
  root.appendChild(h('div', { class: 'page-head' },
    h('div', { class: 'crumbs' },
      h('a', { href: '#/', class: 'back' }, '← Projects'),
      h('h1', {}, 'Settings'))));
  root.appendChild(layout);

  body.appendChild(h('div', { class: 'loading pad' }, spinner()));

  let data;
  try {
    data = await get('/api/settings');
  } catch (e) {
    clear(body);
    body.appendChild(h('div', { class: 'error-box' }, e.message));
    return;
  }

  clear(body);
  if (sectionId === 'server') body.appendChild(serverCard(data.server));
  else if (sectionId === 'defaults') body.appendChild(defaultsCard(data.defaults, ctx));
  else if (sectionId === 'config') body.appendChild(configCard(data.config));
}

function renderHub(root) {
  root.appendChild(h('div', { class: 'page-head' },
    h('div', {},
      h('h1', {}, 'Settings'),
      h('div', { class: 'muted sub' }, 'master configuration — server, defaults, and maia config'))));

  const list = h('div', { class: 'hub-list' });
  for (const s of SECTIONS) {
    list.appendChild(h('a', { class: 'hub-row', href: `#/settings/${s.id}` },
      h('span', { class: 'hub-ico', html: HUB_ICONS[s.icon] }),
      h('span', { class: 'hub-text' },
        h('span', { class: 'hub-title' }, s.title),
        h('span', { class: 'hub-sub muted tiny' }, s.sub)),
      h('span', { class: 'chev', html: HUB_ICONS.chev })));
  }
  root.appendChild(list);
}

function serverCard(s) {
  return section('Server',
    row('Version', s.version),
    row('Listening', `${s.addr}:${s.port}${s.token_set ? ' (token required)' : ' — loopback, no token'}`),
    row('Host', s.hostname || '—'));
}

function defaultsCard(d, ctx) {
  const cap = h('input', { class: 'input input-num2', type: 'number', min: '0' });
  cap.value = d.max_active_worktrees || 0;
  const labels = h('input', { class: 'input grow' });
  labels.value = (d.github.labels || []).join(', ');
  const importMode = h('select', { class: 'input' },
    h('option', { value: 'manual' }, 'manual — import from the Issues screen'),
    h('option', { value: 'auto' }, 'auto — import detected issues up to the cap'));
  importMode.value = d.github.import_mode || 'manual';
  const poll = h('input', { class: 'input input-num2', type: 'number', min: '1' });
  poll.value = d.github.poll_interval_min || 15;

  const save = async () => {
    try {
      await put('/api/settings', {
        defaults: {
          max_active_worktrees: Number(cap.value) || 0,
          github: {
            enabled: d.github.enabled !== false,
            labels: labels.value.split(',').map((s) => s.trim()).filter(Boolean),
            import_mode: importMode.value,
            poll_interval_min: Number(poll.value) || 15,
            mode: d.github.mode || 'origin',
          },
        },
      });
      toast('Defaults saved — applies to all projects without overrides', 'ok');
      ctx.refreshState();
    } catch (e) { toast(e.message, 'err'); }
  };

  return section('Web defaults (all projects)',
    field('Active worktree cap', cap, 'max_active_worktrees — 0 = unlimited. All CRs remain visible; this only limits materialized worktrees.'),
    field('Detection labels', labels, 'comma-separated; empty = every open issue'),
    field('Import mode', importMode, ''),
    field('Poll interval (min)', poll, 'background scan for labeled issues'),
    h('div', { class: 'btn-row' }, h('button', { class: 'btn btn-primary', onclick: save }, 'Save defaults')));
}

function configCard(c) {
  const env = c.env_override || [];
  const locked = (k) => env.includes(k);
  const mk = (key, type = 'text', placeholder = '') => {
    const inp = h('input', { class: 'input', type, placeholder });
    inp.dataset.key = key;
    inp.disabled = locked(key);
    return inp;
  };
  const model = mk('model', 'text', c.model || '');
  const baseURL = mk('base_url', 'text', c.openai_base_url || '');
  const apiKey = mk('openai_api_key', 'password', c.openai_api_key ? '•••••• set — leave empty to keep' : 'sk-…');
  const brave = mk('brave_api_key', 'password', c.brave_api_key ? '•••••• set — leave empty to keep' : '');
  const ghToken = mk('github_token', 'password', c.github_token ? '•••••• set — leave empty to keep' : 'ghp-… / {cmd:gh auth token}');
  const maxTok = mk('max_tokens', 'number', String(c.max_tokens || ''));
  const headers = h('textarea', { class: 'input', rows: 5 }, JSON.stringify(c.openai_headers || {}, null, 2));

  const envNote = env.length
    ? h('div', { class: 'muted tiny' }, `environment overrides active: ${env.join(', ')} — config file values ignored for these`)
    : null;

  const save = async () => {
    let customHeaders;
    try {
      customHeaders = JSON.parse(headers.value || '{}');
      if (!customHeaders || Array.isArray(customHeaders) || typeof customHeaders !== 'object' ||
          Object.values(customHeaders).some((v) => typeof v !== 'string')) {
        throw new Error('custom headers must be a JSON object with string values');
      }
    } catch (e) { toast(`Invalid custom headers: ${e.message}`, 'err'); return; }
    const cfg = { model: model.value.trim(), openai_base_url: baseURL.value.trim(), openai_headers: customHeaders };
    if (apiKey.value) cfg.openai_api_key = apiKey.value;
    if (brave.value) cfg.brave_api_key = brave.value;
    if (ghToken.value) cfg.github_token = ghToken.value;
    if (maxTok.value) cfg.max_tokens = Number(maxTok.value) || 0;
    try {
      await put('/api/settings', { config: cfg });
      toast('maia config saved', 'ok');
      apiKey.value = brave.value = ghToken.value = '';
      ctx.refreshState();
    } catch (e) { toast(e.message, 'err'); }
  };

  return section('maia config (~/.maia/config.json)',
    envNote,
    field('Model', model, 'e.g. openai/gpt-5.2'),
    field('Base URL', baseURL, 'https://api.openai.com/v1'),
    field('Custom request headers (JSON)', headers, 'Template values may include {maia_session}, a stable ID per project and change request.'),
    field('OpenAI API key', apiKey, ''),
    field('Max tokens', maxTok, 'default 16384'),
    field('Brave API key', brave, 'web search (research)'),
    field('GitHub token', ghToken, 'fallback — gh CLI is preferred'),
    h('div', { class: 'btn-row' },
      h('button', { class: 'btn btn-primary', onclick: save }, 'Save config'),
      h('span', { class: 'muted tiny' }, 'clearing secrets is done by editing config.json directly')));
}

function section(title, ...children) {
  return h('div', { class: 'card settings-card' },
    h('div', { class: 'settings-title' }, title),
    ...children);
}

function row(k, v) {
  return h('div', { class: 'settings-row' },
    h('span', { class: 'muted' }, k),
    h('span', { class: 'mono' }, String(v)));
}

function field(label, input, hint) {
  return h('label', { class: 'settings-field' },
    h('span', { class: 'muted tiny' }, label),
    input,
    hint ? h('span', { class: 'muted tiny hint' }, hint) : null);
}
