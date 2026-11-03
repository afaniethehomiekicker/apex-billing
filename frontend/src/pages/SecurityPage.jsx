import React, { useCallback, useEffect, useState } from 'react';
import { ShieldCheck, ShieldOff, Smartphone, LogOut, Monitor, Download, Copy, KeyRound } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth } from '../lib/auth';
import { fmtDateTime } from '../lib/format';
import { Button, Field, Input, Badge, Card, Spinner, ErrorText, Modal, PasswordInput } from '../components/ui';

// Two-step sign-in (authenticator app) and where you're signed in.

const browserName = (ua = '') => {
  const os = /Windows/.test(ua) ? 'Windows' : /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iPhone / iPad' : /Mac OS/.test(ua) ? 'Mac' : /Linux/.test(ua) ? 'Linux' : '';
  const br = /Edg\//.test(ua) ? 'Edge' : /OPR\//.test(ua) ? 'Opera' : /Chrome\//.test(ua) ? 'Chrome' : /Firefox\//.test(ua) ? 'Firefox' : /Safari\//.test(ua) ? 'Safari' : 'Browser';
  return [br, os].filter(Boolean).join(' on ');
};

// ---- recovery codes, shown once ----
const RecoveryCodes = ({ codes, onDone }) => {
  const [saved, setSaved] = useState(false);
  const text = `APEX Billing recovery codes (each works once)\n\n${codes.join('\n')}\n`;
  const download = () => {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'apex-billing-recovery-codes.txt';
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return (
    <div className="space-y-3">
      <p className="text-sm">If you lose your phone, sign in with one of these codes. <b>Each works once. They are shown only now</b> — print them, or save them somewhere safe (not on this computer's desktop).</p>
      <div className="grid grid-cols-2 gap-2 p-3 rounded-lg bg-zinc-100 dark:bg-zinc-900 font-mono text-sm text-center">
        {codes.map(c => <div key={c}>{c}</div>)}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="secondary" icon={Download} onClick={download}>Download</Button>
        <Button variant="secondary" icon={Copy} onClick={() => navigator.clipboard?.writeText(text)}>Copy</Button>
      </div>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} /> I have saved these codes</label>
      <Button disabled={!saved} onClick={onDone}>Done</Button>
    </div>
  );
};

// ---- set up the app ----
export const TwoStepSetup = ({ forced = false, onDone }) => {
  const { setUser, signOut } = useAuth();
  const [setup, setSetup] = useState(null);
  const [code, setCode] = useState('');
  const [codes, setCodes] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    api('/api/me/2fa/setup', { method: 'POST', body: {} }).then(setSetup).catch(err => setError(err.message));
  }, []);
  const enable = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const d = await api('/api/me/2fa/enable', { method: 'POST', body: { code } });
      setCodes({ codes: d.recovery_codes, user: d.user });
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const body = codes ? (
    <RecoveryCodes codes={codes.codes} onDone={() => { setUser(codes.user); onDone?.(); }} />
  ) : (
    <div className="space-y-4">
      <ol className="text-sm space-y-1 list-decimal pl-5">
        <li>Install an authenticator app on your phone (Google Authenticator, Microsoft Authenticator or Authy).</li>
        <li>In the app, add an account and scan this QR code.</li>
        <li>Enter the 6-digit code the app shows.</li>
      </ol>
      {!setup ? (!error && <Spinner />) : (
        <div className="flex flex-col sm:flex-row gap-4 items-center">
          <img alt="QR code for the authenticator app" className="w-44 h-44 rounded-lg border border-zinc-200 bg-white"
            src={`data:image/svg+xml;utf8,${encodeURIComponent(setup.qr_svg)}`} />
          <div className="text-xs text-zinc-600 dark:text-zinc-400 space-y-1">
            <div>Can't scan? Choose "enter a setup key" in the app and type:</div>
            <div className="font-mono text-sm text-zinc-900 dark:text-zinc-100 break-all select-all">{setup.secret}</div>
            <div>Type: time-based</div>
          </div>
        </div>
      )}
      <form onSubmit={enable} className="space-y-3">
        <Field label="Code from the app">
          <Input value={code} onChange={(e) => setCode(e.target.value)} inputMode="numeric" autoComplete="one-time-code" maxLength={7}
            placeholder="123456" className="max-w-[10rem] text-center text-lg tracking-widest font-mono" required />
        </Field>
        <ErrorText>{error}</ErrorText>
        <div className="flex gap-2">
          <Button type="submit" busy={busy} icon={ShieldCheck} disabled={!setup}>Turn on</Button>
          {forced && <Button variant="ghost" onClick={() => signOut('')}>Sign out</Button>}
        </div>
      </form>
    </div>
  );

  if (!forced) return body;
  return (
    <div className="min-h-full flex items-center justify-center p-4">
      <div className="w-full max-w-lg rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-sm p-6 space-y-3">
        <h1 className="text-lg font-bold flex items-center gap-2"><Smartphone className="w-5 h-5 text-indigo-500" /> Set up two-step sign-in</h1>
        <p className="text-sm text-zinc-500">Your company requires a code from your phone when you sign in, as well as your password.</p>
        {body}
      </div>
    </div>
  );
};

// Password + current code, for turning it off or new recovery codes.
const ConfirmForm = ({ title, action, onClose, onDone, danger }) => {
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const save = async () => {
    setBusy(true);
    setError('');
    try { onDone(await action(password, code)); } catch (err) { setError(err.message); } finally { setBusy(false); }
  };
  return (
    <Modal title={title} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant={danger ? 'danger' : 'primary'} busy={busy} onClick={save}>{title}</Button></>}>
      <div className="space-y-3">
        <Field label="Your password"><PasswordInput autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} /></Field>
        <Field label="Code from the app (or a recovery code)"><Input value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" maxLength={12} /></Field>
        <ErrorText>{error}</ErrorText>
      </div>
    </Modal>
  );
};

// ---- on My account ----
export const SecurityPanel = () => {
  const { user, setUser } = useAuth();
  const [settingUp, setSettingUp] = useState(false);
  const [dialog, setDialog] = useState(null); // disable | codes
  const [newCodes, setNewCodes] = useState(null);
  const [sessions, setSessions] = useState(null);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    api('/api/me/sessions').then(setSessions).catch(err => setError(err.message));
  }, []);
  useEffect(() => { load(); }, [load]);

  const revoke = async (id) => {
    try { await api(`/api/me/sessions/${id}`, { method: 'DELETE' }); load(); } catch (err) { setError(err.message); }
  };
  const revokeOthers = async () => {
    if (!window.confirm('Sign out on every other computer and phone?')) return;
    try { await api('/api/me/sessions/revoke-others', { method: 'POST', body: {} }); load(); } catch (err) { setError(err.message); }
  };

  return (
    <div className="space-y-5 max-w-2xl">
      <Card className="p-5 space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-semibold flex items-center gap-2"><Smartphone className="w-4 h-4 text-indigo-500" /> Two-step sign-in</h2>
          {user.totp_enabled ? <Badge tone="green">On</Badge> : <Badge tone="amber">Off</Badge>}
        </div>
        {newCodes ? (
          <RecoveryCodes codes={newCodes} onDone={() => setNewCodes(null)} />
        ) : user.totp_enabled ? (
          <>
            <p className="text-sm text-zinc-600 dark:text-zinc-400">Signing in needs your password and a code from your phone. Recovery codes left: <b>{user.recovery_codes_left}</b>{user.recovery_codes_left <= 3 && <span className="text-amber-600"> — make new ones</span>}.</p>
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" icon={KeyRound} onClick={() => setDialog('codes')}>New recovery codes</Button>
              <Button variant="secondary" icon={ShieldOff} onClick={() => setDialog('disable')}>Turn off</Button>
            </div>
          </>
        ) : settingUp ? (
          <TwoStepSetup onDone={() => setSettingUp(false)} />
        ) : (
          <>
            <p className="text-sm text-zinc-600 dark:text-zinc-400">Protect your account even if your password is stolen: signing in will also need a code from your phone.</p>
            <Button icon={ShieldCheck} onClick={() => setSettingUp(true)}>Set up</Button>
          </>
        )}
      </Card>

      <Card className="p-5 space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-semibold flex items-center gap-2"><Monitor className="w-4 h-4 text-indigo-500" /> Where you're signed in</h2>
          {sessions?.sessions?.length > 1 && <Button variant="secondary" icon={LogOut} onClick={revokeOthers}>Sign out everywhere else</Button>}
        </div>
        <ErrorText>{error}</ErrorText>
        {!sessions ? <Spinner /> : (
          <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
            {sessions.sessions.map(s => (
              <li key={s.id} className="py-2 flex flex-wrap items-center justify-between gap-2 text-sm">
                <span>
                  <b>{browserName(s.user_agent)}</b> {s.current && <Badge tone="indigo">This browser</Badge>}
                  <div className="text-xs text-zinc-500">{s.ip} · signed in {fmtDateTime(s.created_at)} · last active {fmtDateTime(s.last_seen_at)}</div>
                </span>
                {!s.current && <Button variant="ghost" onClick={() => revoke(s.id)}>Sign out</Button>}
              </li>
            ))}
          </ul>
        )}
        {sessions?.recent?.length > 0 && (
          <details>
            <summary className="text-xs font-semibold text-zinc-500 cursor-pointer">Recent sign-in activity</summary>
            <ol className="mt-2 space-y-1">
              {sessions.recent.map(h => (
                <li key={h.id} className={`text-xs ${h.action !== 'login' && h.action !== 'logout' ? 'text-rose-600' : 'text-zinc-600 dark:text-zinc-400'}`}>
                  {fmtDateTime(h.created_at)} · {h.action.replace('_', ' ')} · {h.details} · {h.ip}
                </li>
              ))}
            </ol>
            <p className="mt-2 text-[11px] text-zinc-500">See a sign-in you don't recognise? Change your password now and tell your admin.</p>
          </details>
        )}
      </Card>

      {dialog === 'disable' && (
        <ConfirmForm title="Turn off two-step sign-in" danger
          action={(password, code) => api('/api/me/2fa/disable', { method: 'POST', body: { password, code } })}
          onClose={() => setDialog(null)} onDone={(d) => { setDialog(null); setUser(d.user); }} />
      )}
      {dialog === 'codes' && (
        <ConfirmForm title="Make new recovery codes"
          action={(password, code) => api('/api/me/2fa/recovery-codes', { method: 'POST', body: { password, code } })}
          onClose={() => setDialog(null)} onDone={(d) => { setDialog(null); setUser(d.user); setNewCodes(d.recovery_codes); }} />
      )}
    </div>
  );
};
