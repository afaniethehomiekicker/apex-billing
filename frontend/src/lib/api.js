// Talking to the APEX Billing server.
//
// The session lives in an HttpOnly cookie the server sets: page scripts
// never see it, and the browser sends it only to this site. Every request
// carries the app's own header, which the server requires for anything
// that changes data (cross-site pages can't add it).

const APP_HEADER = { 'X-Requested-With': 'apex-billing' };

let onSessionEnded = () => {};
let onGate = () => {};
export const setSessionEndedHandler = (fn) => { onSessionEnded = fn; };
// Called when the server says "choose a new password / set up two-step
// sign-in first", so the app can show that screen.
export const setGateHandler = (fn) => { onGate = fn; };

const NO_SESSION_PATHS = ['/api/auth/login', '/api/auth/mfa', '/api/me'];

// api(path, { method, body (object → JSON), form (FormData), signal })
// Returns the parsed JSON; throws an Error with the server's message.
export async function api(path, { method = 'GET', body, form, signal } = {}) {
  const headers = { ...APP_HEADER };
  let payload;
  if (form) payload = form;
  else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    payload = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(path, { method, headers, body: payload, signal, credentials: 'same-origin', cache: 'no-store' });
  } catch (err) {
    if (err?.name === 'AbortError') throw err;
    throw new Error('Could not reach the server. Check your connection.');
  }
  const data = await res.json().catch(() => ({}));
  if (res.status === 401 && !NO_SESSION_PATHS.includes(path)) {
    onSessionEnded(data.error || 'Your session has ended. Please sign in again.');
  }
  if (res.status === 403 && (data.code === 'password_change_required' || data.code === 'mfa_setup_required')) {
    onGate(data.code);
  }
  if (!res.ok) {
    const e = new Error(data.error || `Something went wrong (${res.status})`);
    e.status = res.status;
    e.data = data;
    throw e;
  }
  return data;
}

// Query string from an object, leaving out empty values.
export const qs = (params) => {
  const p = new URLSearchParams();
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null && String(v).trim() !== '') p.set(k, String(v).trim());
  });
  const s = p.toString();
  return s ? `?${s}` : '';
};

async function fetchFile(path) {
  const res = await fetch(path, { headers: APP_HEADER, credentials: 'same-origin', cache: 'no-store' });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    if (res.status === 401) onSessionEnded(data.error || 'Your session has ended. Please sign in again.');
    throw new Error(data.error || `Could not load the file (${res.status})`);
  }
  return res;
}

// A file from the server (e.g. a payment slip) as an object URL. Call
// URL.revokeObjectURL when done.
export async function apiBlobURL(path) {
  const res = await fetchFile(path);
  return URL.createObjectURL(await res.blob());
}

// Download a file from the server (e.g. an Excel report), saving it under
// the name the server gives.
export async function apiDownload(path, fallbackName = 'download') {
  const res = await fetchFile(path);
  const cd = res.headers.get('Content-Disposition') || '';
  const m = /filename="([^"]+)"/.exec(cd);
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = m ? m[1] : fallbackName;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
