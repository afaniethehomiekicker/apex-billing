import React, { useState } from 'react';
import { Receipt, LogIn, ShieldCheck } from 'lucide-react';
import { api } from '../lib/api';
import { Button, Field, Input, ErrorText, PasswordInput } from '../components/ui';

// Sign in: email and password, then (when it's on) the code from the
// authenticator app or a recovery code.
export const LoginPage = ({ notice, onSignedIn }) => {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [ticket, setTicket] = useState('');
  const [code, setCode] = useState('');
  const [useRecovery, setUseRecovery] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const d = await api('/api/auth/login', { method: 'POST', body: { email, password } });
      setPassword('');
      if (d.mfa_required) { setTicket(d.mfa_ticket); return; }
      onSignedIn(d.user);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  const submitCode = async (e) => {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const d = await api('/api/auth/mfa', { method: 'POST', body: { mfa_ticket: ticket, code } });
      onSignedIn(d.user);
    } catch (err) {
      setError(err.message);
      setCode('');
      if (err.data?.restart) setTicket('');
    } finally {
      setBusy(false);
    }
  };

  const box = 'w-full max-w-sm rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-sm p-6 space-y-4';
  const brand = (
    <div className="flex items-center gap-3">
      <div className="w-10 h-10 rounded-xl bg-indigo-600 flex items-center justify-center text-white"><Receipt className="w-5 h-5" /></div>
      <div>
        <h1 className="text-lg font-bold text-zinc-900 dark:text-zinc-100">APEX Billing</h1>
        <p className="text-xs text-zinc-500">{ticket ? 'Two-step sign-in' : 'Sign in to continue'}</p>
      </div>
    </div>
  );

  return (
    <div className="min-h-full flex items-center justify-center p-4">
      {!ticket ? (
        <form onSubmit={submit} className={box}>
          {brand}
          {notice && <p className="text-sm text-amber-700 dark:text-amber-400">{notice}</p>}
          <Field label="Email">
            <Input type="email" autoComplete="username" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} required maxLength={200} />
          </Field>
          <Field label="Password">
            <PasswordInput autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required maxLength={200} />
          </Field>
          <ErrorText>{error}</ErrorText>
          <Button type="submit" busy={busy} icon={LogIn} className="w-full">Sign in</Button>
          <p className="text-[11px] text-zinc-500">For your security you are signed out after a period without activity. Don't sign in on shared computers.</p>
        </form>
      ) : (
        <form onSubmit={submitCode} className={box}>
          {brand}
          <p className="text-sm text-zinc-600 dark:text-zinc-400">
            {useRecovery ? 'Enter one of your recovery codes (each works once).' : 'Open your authenticator app and enter the 6-digit code for APEX Billing.'}
          </p>
          <Field label={useRecovery ? 'Recovery code' : 'Code'}>
            <Input autoFocus value={code} onChange={(e) => setCode(e.target.value)} required maxLength={useRecovery ? 12 : 7}
              inputMode={useRecovery ? 'text' : 'numeric'} autoComplete="one-time-code" placeholder={useRecovery ? 'xxxx-xxxx' : '123456'}
              className="text-center text-lg tracking-widest font-mono" />
          </Field>
          <ErrorText>{error}</ErrorText>
          <Button type="submit" busy={busy} icon={ShieldCheck} className="w-full">Verify</Button>
          <div className="flex justify-between text-xs">
            <button type="button" className="text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer" onClick={() => { setUseRecovery(r => !r); setCode(''); setError(''); }}>
              {useRecovery ? 'Use the app code' : 'Lost your phone? Use a recovery code'}
            </button>
            <button type="button" className="text-zinc-500 hover:underline cursor-pointer" onClick={() => { setTicket(''); setCode(''); setError(''); }}>Start again</button>
          </div>
        </form>
      )}
    </div>
  );
};
