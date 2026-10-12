// jobs.js — live console (SSE with replay) + job start helper.

import { get, post, eventsUrl } from './api.js';
import { h, clear, spinner } from './ui.js';

// consoleEl() creates an empty console widget; mount(jobId) starts streaming.
const INSTANCES = new Set();

export function closeAllConsoles() {
  for (const c of [...INSTANCES]) c.close();
}

export function consoleEl() {
  const head = h('div', { class: 'console-head' },
    h('span', { class: 'console-title' }, 'Console'),
    h('span', { class: 'console-status muted' }, 'idle'),
    h('span', { class: 'spacer' }));
  const out = h('pre', { class: 'console-out' });
  const wrap = h('div', { class: 'console' }, head, h('div', { class: 'console-scroll' }, out));

  let es = null, curId = null, follow = true, done = 0;

  out.addEventListener('scroll', () => {
    const nearBottom = out.scrollTop + out.clientHeight >= out.scrollHeight - 40;
    follow = nearBottom;
    followBtn.classList.toggle('on', follow);
  });
  const followBtn = h('button', {
    class: 'btn btn-tiny on', title: 'Auto-follow output',
    onclick: () => { follow = true; out.scrollTop = out.scrollHeight; followBtn.classList.add('on'); },
  }, '↓ follow');
  head.appendChild(followBtn);

  const cancelBtn = h('button', {
    class: 'btn btn-tiny btn-danger hidden', title: 'Stop this command',
    onclick: async () => {
      if (!curId) return;
      try { await post(`/api/jobs/${curId}/cancel`); } catch (e) { /* already gone */ }
    },
  }, 'Stop');
  head.insertBefore(cancelBtn, followBtn);

  function line(text) {
    out.appendChild(document.createTextNode(text + '\n'));
    if (follow) out.scrollTop = out.scrollHeight;
  }

  function stopES() {
    if (es) { es.close(); es = null; }
  }

  function mount(jobId, { onEnd } = {}) {
    stopES();
    curId = jobId;
    done = 0;
    clear(out);
    follow = true;
    out.scrollTop = 0;
    head.querySelector('.console-status').innerHTML = '';
    head.querySelector('.console-status').appendChild(spinner());
    head.querySelector('.console-status').appendChild(document.createTextNode(' running'));
    cancelBtn.classList.remove('hidden');
    followBtn.classList.add('on');

    es = new EventSource(eventsUrl(jobId, 0));
    es.onmessage = (e) => {
      const ev = JSON.parse(e.data);
      if (ev.type === 'line') { line(ev.data); done = Math.max(done, ev.id); }
      else if (ev.type === 'status') { /* reserved */ }
      else if (ev.type === 'end') {
        const status = ev.data;
        finish(status);
        es.close(); es = null;
        if (onEnd) onEnd(status);
      }
    };
    es.onerror = () => {
      // EventSource retries automatically; if the job is gone the server
      // closes the stream and 'end' arrives via replay on reconnect.
    };
  }

  function finish(status) {
    const el = head.querySelector('.console-status');
    el.innerHTML = '';
    el.textContent = status;
    el.className = 'console-status status-' + status;
    cancelBtn.classList.add('hidden');
    curId = null;
  }

  // renderExisting shows a past job's status without streaming.
  function renderExisting(summary) {
    stopES();
    out.textContent = '';
    const el = head.querySelector('.console-status');
    el.textContent = summary.status;
    el.className = 'console-status status-' + summary.status;
    line(`last: ${summary.command} (${summary.status}, ${summary.lines} lines)`);
  }

  function close() {
    stopES();
    cancelBtn.classList.add('hidden');
    INSTANCES.delete(api);
  }

  const api = { el: wrap, mount, close, finish, renderExisting };
  INSTANCES.add(api);
  return api;
}

// startJob fires a project job and returns the job summary (409 => error).
export async function startJob(pid, body) {
  return post(`/api/projects/${pid}/jobs`, body);
}

// latestJobFor returns the newest job for a project+slug (or kind), if any.
export async function latestJobFor(pid, slug, kinds) {
  const jobs = await get('/api/jobs');
  return jobs.find((j) =>
    j.project_id === pid &&
    (!slug || j.slug === slug) &&
    (!kinds || kinds.includes(j.kind))) || null;
}
