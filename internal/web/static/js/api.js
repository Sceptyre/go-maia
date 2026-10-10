// api.js — HTTP client with token handling (bearer header; query token for SSE).

let token = sessionStorage.getItem('maia_token') || '';

export function getToken() { return token; }

export function setToken(t) {
  token = t || '';
  if (token) sessionStorage.setItem('maia_token', token);
  else sessionStorage.removeItem('maia_token');
}

async function req(method, path, body) {
  const headers = { 'Accept': 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (token) headers['Authorization'] = 'Bearer ' + token;
  let res;
  try {
    res = await fetch(path, {
      method, headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    throw Object.assign(new Error('network error — is the server running?'), { code: 0 });
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw Object.assign(new Error((data && data.error) || res.statusText), { code: res.status, data });
  }
  return data;
}

export const get = (p) => req('GET', p);
export const post = (p, b) => req('POST', p, b === undefined ? {} : b);
export const put = (p, b) => req('PUT', p, b === undefined ? {} : b);
export const del = (p) => req('DELETE', p);

// EventSource cannot set headers — the server accepts ?token= for SSE.
export function eventsUrl(jobId, after) {
  let u = `/api/jobs/${encodeURIComponent(jobId)}/events?after=${after || 0}`;
  if (token) u += `&token=${encodeURIComponent(token)}`;
  return u;
}

export async function state() { return get('/api/state'); }

// withToken appends the token to an href (for links opened in a new tab).
export function withToken(href) {
  if (!token) return href;
  return href + (href.includes('?') ? '&' : '?') + 'token=' + encodeURIComponent(token);
}
