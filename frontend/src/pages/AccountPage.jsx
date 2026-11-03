import React, { useState } from 'react';
import { UserCircle, Save, Mail } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth } from '../lib/auth';
import { PageHeader, Card, Field, Input, Button, ErrorText, PasswordInput } from '../components/ui';
import { AvatarEditor } from '../components/Avatar';
import { ChangePasswordPage } from './ChangePasswordPage';
import { SecurityPanel } from './SecurityPage';

// My account: profile (picture, name, phone), sign-in email (admins can
// change their own), password, two-step sign-in and sessions.

const Saved = ({ children }) => (children ? <p className="text-sm text-emerald-700 dark:text-emerald-400">{children}</p> : null);

const ProfileCard = () => {
  const { user, setUser } = useAuth();
  const [form, setForm] = useState({ name: user.name, phone: user.phone || '' });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');
  const changed = form.name !== user.name || form.phone !== (user.phone || '');
  const save = async (e) => {
    e.preventDefault();
    setError(''); setDone('');
    setBusy(true);
    try {
      const d = await api('/api/me/profile', { method: 'PUT', body: form });
      setUser(d.user);
      setForm({ name: d.user.name, phone: d.user.phone || '' });
      setDone('Profile saved.');
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card className="p-5 space-y-5">
      <h2 className="text-sm font-semibold">Profile</h2>
      <AvatarEditor user={user} base="/api/me" onChanged={setUser} />
      <form onSubmit={save} className="space-y-3">
        <Field label="Name" required><Input value={form.name} onChange={(e) => setForm(f => ({ ...f, name: e.target.value }))} maxLength={150} required /></Field>
        <Field label="Phone"><Input value={form.phone} onChange={(e) => setForm(f => ({ ...f, phone: e.target.value }))} maxLength={40} /></Field>
        <div className="text-xs text-zinc-500 space-y-0.5">
          <div>Role: <b className="text-zinc-700 dark:text-zinc-300">{user.role_label}</b></div>
          <div>User ID: <span className="font-mono">{user.user_number}</span></div>
        </div>
        <ErrorText>{error}</ErrorText>
        <Saved>{done}</Saved>
        <Button type="submit" icon={Save} busy={busy} disabled={!changed}>Save profile</Button>
      </form>
    </Card>
  );
};

const EmailCard = () => {
  const { user, setUser, can } = useAuth();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');
  const admin = can('manage_users');
  const save = async (e) => {
    e.preventDefault();
    setError(''); setDone('');
    setBusy(true);
    try {
      const d = await api('/api/me/email', { method: 'PUT', body: { email, current_password: password, code } });
      setUser(d.user);
      setEmail(''); setPassword(''); setCode('');
      setDone(`You now sign in with ${d.user.email}. You were signed out on your other computers and phones.`);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card className="p-5 space-y-3">
      <h2 className="text-sm font-semibold">Sign-in email</h2>
      <div className="text-sm">You sign in with <b>{user.email}</b></div>
      {!admin ? (
        <p className="text-xs text-zinc-500">Your sign-in email can't be changed. Ask an admin if it's wrong; they can set up a new account for you.</p>
      ) : (
        <form onSubmit={save} className="space-y-3">
          <Field label="New email" required>
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} maxLength={200} autoComplete="email" required />
          </Field>
          <Field label="Current password" required hint="To confirm it's you.">
            <PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
          </Field>
          {user.totp_enabled && (
            <Field label="Code from your authenticator app" required>
              <Input inputMode="numeric" value={code} onChange={(e) => setCode(e.target.value)} maxLength={20} autoComplete="one-time-code" required />
            </Field>
          )}
          <ErrorText>{error}</ErrorText>
          <Saved>{done}</Saved>
          <Button type="submit" icon={Mail} busy={busy}>Change email</Button>
        </form>
      )}
    </Card>
  );
};

export const AccountPage = () => (
  <>
    <PageHeader icon={UserCircle} title="My account" subtitle="Your profile, sign-in details and security." />
    <div className="grid gap-5 lg:grid-cols-2 items-start mb-5">
      <ProfileCard />
      <div className="space-y-5">
        <EmailCard />
        <div>
          <h2 className="text-sm font-semibold mb-2">Change password</h2>
          <ChangePasswordPage />
        </div>
      </div>
    </div>
    <SecurityPanel />
  </>
);
