import React, { useCallback, useEffect, useState } from 'react';
import { Users, Plus, Pencil, KeyRound, ShieldOff, LogOut } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth } from '../lib/auth';
import { fmtDateTime } from '../lib/format';
import { PageHeader, Button, Field, Input, Select, Badge, Modal, Table, Spinner, ErrorText, PasswordInput } from '../components/ui';
import { Avatar, AvatarEditor } from '../components/Avatar';

// Users of APEX Billing (admins only). Nobody is deleted: disabling an
// account blocks sign-in and keeps its history.

const ROLE_HELP = {
  admin: 'Everything, including users and company settings.',
  accountant: 'Clients, invoices, payments and reports.',
  recorder: 'Sees clients and invoices; records payments with the slip.',
  verifier: 'Sees clients and invoices; verifies or rejects payments; reports.',
};

const UserForm = ({ user: initial, roles, meId, onClose, onSaved, onPictureChanged }) => {
  const [user, setUser] = useState(initial);
  const [form, setForm] = useState({
    name: user?.name || '', email: user?.email || '', phone: user?.phone || '',
    role: user?.role || 'recorder', active: user ? user.active : true, password: '',
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));

  const save = async () => {
    setError('');
    setBusy(true);
    try {
      const d = user
        ? await api(`/api/users/${user.id}`, { method: 'PUT', body: { name: form.name, phone: form.phone, role: form.role, active: form.active } })
        : await api('/api/users', { method: 'POST', body: form });
      onSaved(d.user);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title={user ? `Edit ${user.name}` : 'Add user'} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} onClick={save}>{user ? 'Save' : 'Add user'}</Button></>}>
      <div className="space-y-3">
        {user && (
          <AvatarEditor user={user} base={`/api/users/${user.id}`}
            onChanged={(u) => { setUser(x => ({ ...x, has_avatar: u.has_avatar, avatar_version: u.avatar_version })); onPictureChanged(u); }} />
        )}
        <Field label="Name" required><Input autoFocus value={form.name} onChange={set('name')} maxLength={150} /></Field>
        <Field label="Email" required hint={!user ? 'They sign in with this.' : user.id === meId ? 'Change your own sign-in email on My account.' : 'Other users’ emails can’t be changed.'}>
          <Input type="email" value={form.email} onChange={set('email')} disabled={!!user} maxLength={200} />
        </Field>
        <Field label="Phone"><Input value={form.phone} onChange={set('phone')} maxLength={40} /></Field>
        <Field label="Role" required hint={ROLE_HELP[form.role]}>
          <Select value={form.role} onChange={set('role')}>
            {roles.map(r => <option key={r.key} value={r.key}>{r.label}</option>)}
          </Select>
        </Field>
        {!user && (
          <Field label="Temporary password" required hint="At least 10 characters with letters and numbers, not their name or email. They choose their own at first sign-in. Give it to them in person or by phone, not by email.">
            <PasswordInput value={form.password} onChange={set('password')} autoComplete="new-password" />
          </Field>
        )}
        {user && (
          <label className="flex items-center gap-2 text-sm select-none">
            <input type="checkbox" checked={form.active} onChange={set('active')} /> Active (can sign in)
          </label>
        )}
        <ErrorText>{error}</ErrorText>
      </div>
    </Modal>
  );
};

const PasswordForm = ({ user, onClose }) => {
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');
  const save = async () => {
    setError('');
    setBusy(true);
    try {
      const d = await api(`/api/users/${user.id}/password`, { method: 'POST', body: { password } });
      setDone(d.message);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal title={`Set a temporary password for ${user.name}`} onClose={onClose}
      footer={done ? <Button onClick={onClose}>Done</Button> : <><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} onClick={save}>Set password</Button></>}>
      {done ? <p className="text-sm text-emerald-700 dark:text-emerald-400">{done}</p> : (
        <div className="space-y-3">
          <Field label="Temporary password" hint="Their current sessions end; they choose a new password at next sign-in.">
            <PasswordInput autoFocus value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
          </Field>
          <ErrorText>{error}</ErrorText>
        </div>
      )}
    </Modal>
  );
};

export const UsersPage = () => {
  const { user: me, setUser: setMe } = useAuth();
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null); // null | {} | user
  const [pwFor, setPwFor] = useState(null);

  const load = useCallback(() => {
    api('/api/users').then(setData).catch(err => setError(err.message));
  }, []);
  useEffect(() => { load(); }, [load]);
  const userAction = async (u, what, question) => {
    if (!window.confirm(question)) return;
    setError('');
    try { await api(`/api/users/${u.id}/${what}`, { method: 'POST', body: {} }); load(); } catch (err) { setError(err.message); }
  };

  return (
    <>
      <PageHeader icon={Users} title="Users" subtitle="Who can sign in to APEX Billing, and what they can do."
        actions={<Button icon={Plus} onClick={() => setEditing({})}>Add user</Button>} />
      <ErrorText>{error}</ErrorText>
      {!data ? <Spinner /> : (
        <Table head={['Name', 'Email', 'Role', 'Status', 'Last sign-in', '']}>
          {data.users.map(u => (
            <tr key={u.id} className="border-t border-zinc-100 dark:border-zinc-800">
              <td className="px-3 py-2">
                <div className="flex items-center gap-2.5">
                  <Avatar user={u} />
                  <div><div className="font-semibold">{u.name}{u.id === me.id && <span className="text-zinc-500 font-normal"> (you)</span>}</div><div className="font-mono text-[11px] text-zinc-500">{u.user_number}</div></div>
                </div>
              </td>
              <td className="px-3 py-2">{u.email}</td>
              <td className="px-3 py-2">{u.role_label}</td>
              <td className="px-3 py-2">
                {u.active ? <Badge tone="green">Active</Badge> : <Badge>Disabled</Badge>}
                {u.must_change_password && u.active && <span className="ml-1"><Badge tone="amber">Temporary password</Badge></span>}
                {u.active && <span className="ml-1">{u.totp_enabled ? <Badge tone="green">Two-step on</Badge> : <Badge tone={u.mfa_setup_required ? 'amber' : 'zinc'}>Two-step off</Badge>}</span>}
                {u.sessions > 0 && <div className="text-[11px] text-zinc-500 mt-0.5">Signed in on {u.sessions} device{u.sessions === 1 ? '' : 's'}</div>}
              </td>
              <td className="px-3 py-2 whitespace-nowrap text-zinc-600 dark:text-zinc-400">{fmtDateTime(u.last_login_at) || 'Never'}</td>
              <td className="px-3 py-2 whitespace-nowrap text-right">
                <button type="button" title="Edit" onClick={() => setEditing(u)} className="p-1.5 rounded hover:bg-zinc-100 dark:hover:bg-zinc-800 cursor-pointer"><Pencil className="w-4 h-4" /></button>
                {u.id !== me.id && <button type="button" title="Set a temporary password" onClick={() => setPwFor(u)} className="p-1.5 rounded hover:bg-zinc-100 dark:hover:bg-zinc-800 cursor-pointer"><KeyRound className="w-4 h-4" /></button>}
                {u.id !== me.id && u.totp_enabled && <button type="button" title="Reset two-step sign-in (lost phone)" onClick={() => userAction(u, '2fa/reset', `Reset two-step sign-in for ${u.name}? Use this only if they lost their phone and you have confirmed it's really them. Their sessions end and they set it up again.`)} className="p-1.5 rounded hover:bg-zinc-100 dark:hover:bg-zinc-800 cursor-pointer"><ShieldOff className="w-4 h-4" /></button>}
                {u.id !== me.id && u.sessions > 0 && <button type="button" title="Sign out everywhere" onClick={() => userAction(u, 'signout', `Sign ${u.name} out on every computer and phone?`)} className="p-1.5 rounded hover:bg-zinc-100 dark:hover:bg-zinc-800 cursor-pointer"><LogOut className="w-4 h-4" /></button>}
              </td>
            </tr>
          ))}
        </Table>
      )}
      {editing && data && (
        <UserForm user={editing.id ? editing : null} roles={data.roles} meId={me.id} onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); load(); }}
          onPictureChanged={(u) => { load(); if (u.id === me.id) setMe(u); }} />
      )}
      {pwFor && <PasswordForm user={pwFor} onClose={() => setPwFor(null)} />}
    </>
  );
};
