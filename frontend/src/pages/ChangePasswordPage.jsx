import React, { useState } from 'react';
import { KeyRound } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth } from '../lib/auth';
import { Button, Field, Input, ErrorText, Card, PasswordInput } from '../components/ui';

// Change your own password. `forced`: shown instead of the app when an admin
// set a temporary password (or at the first admin's first sign-in).
export const ChangePasswordPage = ({ forced = false }) => {
  const { setUser, signOut } = useAuth();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [again, setAgain] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    setDone('');
    if (next !== again) { setError("The new passwords don't match"); return; }
    setBusy(true);
    try {
      const d = await api('/api/me/password', { method: 'PUT', body: { current_password: current, new_password: next } });
      setUser(d.user);
      setCurrent(''); setNext(''); setAgain('');
      setDone('Password changed. You were signed out on your other computers and phones.');
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const form = (
    <form onSubmit={submit} className="space-y-4">
      <Field label={forced ? 'Temporary password' : 'Current password'} required>
        <PasswordInput autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
      </Field>
      <Field label="New password" required hint="At least 10 characters with letters and numbers. Not a common password, and not your name or email. A short sentence works well.">
        <PasswordInput autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} required />
      </Field>
      <Field label="New password again" required>
        <PasswordInput autoComplete="new-password" value={again} onChange={(e) => setAgain(e.target.value)} required />
      </Field>
      <ErrorText>{error}</ErrorText>
      {done && <p className="text-sm text-emerald-700 dark:text-emerald-400">{done}</p>}
      <div className="flex gap-2">
        <Button type="submit" busy={busy} icon={KeyRound}>Change password</Button>
        {forced && <Button variant="ghost" onClick={() => signOut('')}>Sign out</Button>}
      </div>
    </form>
  );

  if (!forced) return <Card className="p-5 max-w-md">{form}</Card>;
  return (
    <div className="min-h-full flex items-center justify-center p-4">
      <div className="w-full max-w-sm rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-sm p-6 space-y-3">
        <h1 className="text-lg font-bold">Choose a new password</h1>
        <p className="text-sm text-zinc-500">You signed in with a temporary password. Choose your own to continue.</p>
        {form}
      </div>
    </div>
  );
};
