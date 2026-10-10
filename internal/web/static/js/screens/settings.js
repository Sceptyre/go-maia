// screens/settings.js — admin dashboard: server info, defaults, maia config.

import { get, put } from '../api.js';
import { h, clear, toast, spinner } from '../ui.js';

export async function render(root, ctx) {
  clear(root);
  root.appendChild(h('div', { class: 'page-head' },
    h('div', { class: 'crumbs' },
      h('a', { href: '#/', class: 'back' }, '← Projects'),
      h('h1', {}, 'Settings'),
      h('div', { class: 'muted sub' }, 'master configuration — one place for server, defaults, and maia config'))));

  const body = h('div', { class: 'settings' });
  root.appendChild(body);
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
  body.appendChild(serverCard(data.server));
  body.appendChild(defaultsCard(data.defaults, ctx));
  body.appendChild(configCard(data.config));
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
    field('Active worktree cap', cap, 'max_active_worktrees — 0 = unlimited. Auto-import stops at the cap; lazy materialization keeps the rest waiting in Issues.'),
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

  const envNote = env.length
    ? h('div', { class: 'muted tiny' }, `environment overrides active: ${env.join(', ')} — config file values ignored for these`)
    : null;

  const save = async () => {
    const cfg = { model: model.value.trim(), openai_base_url: baseURL.value.trim() };
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
