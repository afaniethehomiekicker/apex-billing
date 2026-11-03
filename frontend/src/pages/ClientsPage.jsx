import React, { useCallback, useEffect, useState } from 'react';
import { Building2, User, Plus, Search, Pencil, Archive, RotateCcw, History, FileSpreadsheet } from 'lucide-react';
import { api, qs } from '../lib/api';
import { useAuth, navigate } from '../lib/auth';
import { ClientServices } from './ServicesPage';
import { ClientContracts } from './ContractsPage';
import { ClientInvoices } from './InvoicesPage';
import { ClientPayments } from './PaymentsPage';
import { formatMoney, fmtDay, fmtDateTime, dayOf, PROVINCES, CLIENT_TYPES, formatCNIC, taxIds } from '../lib/format';
import {
  PageHeader, Button, Field, Input, Select, Textarea, Badge, Modal, Pager, Table, Spinner, ErrorText, Card, inputCls,
} from '../components/ui';

// Billing clients: list, add, edit, archive / restore. #/clients/12 opens
// one client.

const STATUS = { active: ['Active', 'green'], inactive: ['Inactive', 'amber'], archived: ['Archived', 'zinc'] };
const StatusBadge = ({ status }) => {
  const [label, tone] = STATUS[status] || [status, 'zinc'];
  return <Badge tone={tone}>{label}</Badge>;
};

// Company or individual: the icon used in lists.
export const ClientTypeIcon = ({ type, className = 'w-4 h-4' }) => (type === 'individual'
  ? <User className={className} aria-label="Individual" /> : <Building2 className={className} aria-label="Company" />);

const EMPTY = {
  client_type: 'company', name: '', contact_person: '', email: '', phone: '', mobile: '', address: '', city: '', province: '',
  ntn: '', strn: '', cnic: '', tax_percent: '0', terms_days: '15', opening_balance: '', opening_balance_date: '',
  notes: '', status: 'active',
};
const fromClient = (c) => ({
  ...EMPTY, ...Object.fromEntries(Object.keys(EMPTY).map(k => [k, c[k] ?? ''])),
  tax_percent: String(c.tax_percent ?? 0), terms_days: String(c.terms_days ?? 15),
  opening_balance: c.opening_balance ? String(c.opening_balance) : '',
  opening_balance_date: dayOf(c.opening_balance_date),
});

// ---- add / edit ----
const ClientForm = ({ client, defaults, onClose, onSaved }) => {
  const [form, setForm] = useState(() => (client ? fromClient(client) : {
    ...EMPTY, tax_percent: String(defaults?.default_tax_percent ?? 0), terms_days: String(defaults?.default_terms_days ?? 15),
  }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [duplicate, setDuplicate] = useState('');
  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.value }));
  const person = form.client_type === 'individual';

  const save = async (confirmDuplicate = false) => {
    setError('');
    const num = (v) => (String(v).trim() === '' ? 0 : Number(String(v).replace(/,/g, '')));
    const body = {
      ...form,
      tax_percent: num(form.tax_percent), terms_days: Math.round(num(form.terms_days)),
      opening_balance: num(form.opening_balance), confirm_duplicate: confirmDuplicate,
    };
    if (!form.name.trim()) { setError(person ? "Enter the person's full name" : 'Enter the company name'); return; }
    if (form.cnic && form.cnic.replace(/\D/g, '').length !== 13) { setError('CNIC must be 13 digits, e.g. 35202-1234567-1'); return; }
    if ([body.tax_percent, body.terms_days, body.opening_balance].some(n => !Number.isFinite(n))) { setError('Check the numbers'); return; }
    setBusy(true);
    try {
      const d = client
        ? await api(`/api/clients/${client.id}`, { method: 'PUT', body })
        : await api('/api/clients', { method: 'POST', body });
      onSaved(d.client);
    } catch (err) {
      if (err.data?.duplicate) setDuplicate(err.message);
      else setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal wide title={client ? `Edit ${client.client_number}` : 'Add client'} onClose={onClose}
      footer={<>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button busy={busy} onClick={() => save(false)}>{client ? 'Save changes' : 'Add client'}</Button>
      </>}>
      <div className="space-y-5">
        <div className="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Client type">
          {[['company', Building2, 'Company', 'A business or organisation'], ['individual', User, 'Individual', 'A person billed in their own name']].map(([v, Icon, label, sub]) => (
            <button key={v} type="button" role="radio" aria-checked={form.client_type === v}
              onClick={() => setForm(f => ({ ...f, client_type: v }))}
              className={`flex items-start gap-2.5 p-3 rounded-lg border text-left cursor-pointer ${form.client_type === v
                ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-950/30 ring-1 ring-indigo-500'
                : 'border-zinc-300 dark:border-zinc-700 hover:bg-zinc-50 dark:hover:bg-zinc-900'}`}>
              <Icon className={`w-5 h-5 mt-0.5 ${form.client_type === v ? 'text-indigo-600' : 'text-zinc-400'}`} />
              <span><span className="block text-sm font-semibold">{label}</span><span className="block text-[11px] text-zinc-500">{sub}</span></span>
            </button>
          ))}
        </div>

        <section className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label={person ? 'Full name' : 'Company name'} required className="sm:col-span-2" hint="As it should appear on invoices.">
            <Input autoFocus value={form.name} onChange={set('name')} maxLength={200} />
          </Field>
          {person ? (
            <Field label="CNIC" hint="13 digits. Printed on invoices.">
              <Input inputMode="numeric" value={form.cnic} onChange={(e) => setForm(f => ({ ...f, cnic: formatCNIC(e.target.value) }))} maxLength={15} placeholder="35202-1234567-1" />
            </Field>
          ) : (
            <Field label="Contact person"><Input value={form.contact_person} onChange={set('contact_person')} maxLength={150} /></Field>
          )}
          <Field label="Billing email" hint="Invoices go here. Several: separate with commas.">
            <Input value={form.email} onChange={set('email')} maxLength={200} />
          </Field>
          <Field label="Phone"><Input value={form.phone} onChange={set('phone')} maxLength={60} /></Field>
          <Field label="Mobile"><Input value={form.mobile} onChange={set('mobile')} maxLength={60} /></Field>
          <Field label="Address" className="sm:col-span-2"><Textarea rows={2} value={form.address} onChange={set('address')} maxLength={1000} /></Field>
          <Field label="City"><Input value={form.city} onChange={set('city')} maxLength={100} /></Field>
          <Field label="Province">
            <Select value={form.province} onChange={set('province')}>
              <option value="">—</option>
              {PROVINCES.map(p => <option key={p} value={p}>{p}</option>)}
            </Select>
          </Field>
        </section>

        <section className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <Field label="NTN" hint={person ? 'Only if the person has one.' : undefined}><Input value={form.ntn} onChange={set('ntn')} maxLength={40} /></Field>
          <Field label="STRN" hint={person ? 'Only if registered for sales tax.' : undefined}><Input value={form.strn} onChange={set('strn')} maxLength={40} /></Field>
          {person ? (
            <Field label="Alternate contact" hint="Optional, e.g. someone to call on their behalf.">
              <Input value={form.contact_person} onChange={set('contact_person')} maxLength={150} />
            </Field>
          ) : (
            <Field label="Owner's CNIC" hint="Optional, e.g. for a sole proprietor.">
              <Input inputMode="numeric" value={form.cnic} onChange={(e) => setForm(f => ({ ...f, cnic: formatCNIC(e.target.value) }))} maxLength={15} />
            </Field>
          )}
        </section>

        <section className="grid grid-cols-1 sm:grid-cols-2 gap-3 p-3 rounded-lg bg-indigo-50/50 dark:bg-indigo-950/20 border border-indigo-200 dark:border-indigo-900">
          <Field label="Payment terms (days)" hint="Due date = invoice date + this many days.">
            <Input inputMode="numeric" value={form.terms_days} onChange={set('terms_days')} maxLength={3} />
          </Field>
          <Field label="Opening balance (PKR)" hint="What they already owed when added here. Negative if they paid in advance.">
            <Input inputMode="decimal" value={form.opening_balance} onChange={set('opening_balance')} maxLength={20} placeholder="0" />
          </Field>
          <Field label="Opening balance as of">
            <Input type="date" value={form.opening_balance_date} onChange={set('opening_balance_date')} />
          </Field>
        </section>

        <section className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <Field label="Notes" className="sm:col-span-2"><Textarea rows={2} value={form.notes} onChange={set('notes')} maxLength={5000} /></Field>
          <Field label="Status" hint="Archived clients are hidden from lists; nothing is deleted.">
            <Select value={form.status} onChange={set('status')}>
              <option value="active">Active</option>
              <option value="inactive">Inactive</option>
              <option value="archived">Archived</option>
            </Select>
          </Field>
        </section>

        {duplicate && (
          <div className="p-3 rounded-lg border border-amber-300 dark:border-amber-800 bg-amber-50 dark:bg-amber-950/30 text-sm text-amber-800 dark:text-amber-300">
            <p>{duplicate}. Is this a different client?</p>
            <div className="mt-2 flex gap-2">
              <Button variant="secondary" onClick={() => setDuplicate('')}>Go back</Button>
              <Button onClick={() => { setDuplicate(''); save(true); }}>Yes, save anyway</Button>
            </div>
          </div>
        )}
        <ErrorText>{error}</ErrorText>
      </div>
    </Modal>
  );
};

// ---- one client ----
const ClientDetail = ({ id, canManage, onEdit, onChanged }) => {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    setError('');
    api(`/api/clients/${id}`).then(setData).catch(err => setError(err.message));
  }, [id]);
  useEffect(() => { load(); }, [load]);

  if (error) return <ErrorText>{error}</ErrorText>;
  if (!data) return <Spinner />;
  const c = data.client;

  const setStatus = async (status) => {
    setBusy(true);
    try {
      await api(`/api/clients/${c.id}`, { method: 'PUT', body: { ...fromClient(c), tax_percent: c.tax_percent, terms_days: c.terms_days, opening_balance: c.opening_balance, status, confirm_duplicate: true } });
      load();
      onChanged();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const row = (label, value) => (
    <div><div className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">{label}</div><div className="text-sm break-words whitespace-pre-wrap">{value || '—'}</div></div>
  );

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="font-mono text-xs text-zinc-500">{c.client_number}</div>
          <h2 className="text-lg font-bold flex items-center gap-2"><ClientTypeIcon type={c.client_type} className="w-5 h-5 text-zinc-400" /> {c.name} <Badge>{CLIENT_TYPES[c.client_type] || 'Company'}</Badge> <StatusBadge status={c.status} /></h2>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" icon={FileSpreadsheet} onClick={() => navigate(`statement/${c.id}`)}>Statement</Button>
        {canManage && (
          <div className="flex gap-2">
            <Button variant="secondary" icon={Pencil} onClick={() => onEdit(c, load)}>Edit</Button>
            {c.status === 'archived'
              ? <Button variant="secondary" icon={RotateCcw} busy={busy} onClick={() => setStatus('active')}>Restore</Button>
              : <Button variant="secondary" icon={Archive} busy={busy} onClick={() => { if (window.confirm(`Archive ${c.name}? It will be hidden from lists; nothing is deleted.`)) setStatus('archived'); }}>Archive</Button>}
          </div>
        )}
        </div>
      </div>
      <Card className="p-4 grid grid-cols-1 sm:grid-cols-3 gap-4">
        {c.client_type === 'individual' ? row('CNIC', c.cnic) : row('Contact person', c.contact_person)}
        {row('Billing email', c.email)}
        {row('Phone / mobile', [c.phone, c.mobile].filter(Boolean).join(' · '))}
        {row('Address', [c.address, c.city, c.province].filter(Boolean).join('\n'))}
        {row('NTN / STRN', [c.ntn && `NTN ${c.ntn}`, c.strn && `STRN ${c.strn}`].filter(Boolean).join(' · '))}
        {c.client_type === 'individual' ? row('Alternate contact', c.contact_person) : row("Owner's CNIC", c.cnic)}
        {row('Payment terms', `${c.terms_days} days`)}
        {row('Opening balance', c.opening_balance ? `${formatMoney(c.opening_balance)} as of ${fmtDay(c.opening_balance_date)}` : '—')}
        {c.notes && <div className="sm:col-span-3">{row('Notes', c.notes)}</div>}
      </Card>
      {data.balance && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          {[
            ['Balance', data.balance.balance, data.balance.balance < 0 ? 'in credit' : 'owed to you', data.balance.balance > 0 ? 'text-zinc-900 dark:text-zinc-100' : 'text-emerald-600'],
            ['Overdue', data.balance.overdue, 'on invoices past due', data.balance.overdue > 0 ? 'text-rose-600' : ''],
            ['Waiting verification', data.balance.pending, 'payments recorded', data.balance.pending > 0 ? 'text-amber-600' : ''],
            ['Unapplied credit', data.balance.unapplied, 'verified, not on an invoice', ''],
          ].map(([label, v, sub, tone]) => (
            <Card key={label} className="p-3">
              <div className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">{label}</div>
              <div className={`text-lg font-bold ${tone}`}>{formatMoney(Math.abs(v))}</div>
              <div className="text-[11px] text-zinc-500">{sub}</div>
            </Card>
          ))}
        </div>
      )}
      <p className="text-[11px] text-zinc-500 -mt-3">Balance = opening balance {data.balance ? formatMoney(data.balance.opening) : ''} + issued invoices − verified payments (incl. tax deducted).</p>
      <ClientContracts client={c} />
      <ClientServices client={c} onChanged={load} />
      <ClientInvoices client={c} />
      <ClientPayments client={c} />
      <div>
        <h3 className="text-sm font-semibold mb-2 flex items-center gap-1.5"><History className="w-4 h-4 text-indigo-500" /> History</h3>
        {data.history.length === 0 ? <p className="text-sm text-zinc-500">No changes recorded.</p> : (
          <ol className="space-y-1.5">
            {data.history.map(h => (
              <li key={h.id} className="text-xs text-zinc-700 dark:text-zinc-300">
                <span className="text-zinc-500">{fmtDateTime(h.created_at)}</span> · <b>{h.user_name}</b> · {h.action} · {h.details}
              </li>
            ))}
          </ol>
        )}
      </div>
    </div>
  );
};

export const ClientsPage = ({ arg }) => {
  const { can } = useAuth();
  const canManage = can('manage_clients');
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  const [status, setStatus] = useState('');
  const [city, setCity] = useState('');
  const [type, setType] = useState('');
  const [page, setPage] = useState(1);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [form, setForm] = useState(null); // null | { client?, after? }
  const [settings, setSettings] = useState(null);
  const [reload, setReload] = useState(0);

  useEffect(() => { const t = setTimeout(() => setDebounced(q.trim()), 300); return () => clearTimeout(t); }, [q]);
  useEffect(() => { setPage(1); }, [debounced, status, city, type]);
  useEffect(() => { api('/api/settings').then(d => setSettings(d.settings)).catch(() => {}); }, []);
  useEffect(() => {
    const ctrl = new AbortController();
    setError('');
    api(`/api/clients${qs({ q: debounced, status, city, type, page })}`, { signal: ctrl.signal })
      .then(setData).catch(err => { if (err.name !== 'AbortError') setError(err.message); });
    return () => ctrl.abort();
  }, [debounced, status, city, type, page, reload]);

  const openId = /^\d+$/.test(arg || '') ? arg : null;

  return (
    <>
      {openId ? (
        <>
          <button type="button" onClick={() => navigate('clients')} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline mb-4 cursor-pointer">← All clients</button>
          <ClientDetail key={openId} id={openId} canManage={canManage}
            onEdit={(client, after) => setForm({ client, after })} onChanged={() => setReload(r => r + 1)} />
        </>
      ) : (
        <>
          <PageHeader icon={Building2} title="Clients" subtitle="Everyone you bill."
            actions={canManage && <Button icon={Plus} onClick={() => setForm({})}>Add client</Button>} />
          <div className="flex flex-wrap items-center gap-2 mb-3">
            <span className="relative flex-1 min-w-[14rem] max-w-md">
              <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
              <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Name, client no., NTN, CNIC, email, phone…" className={`${inputCls} pl-8`} />
            </span>
            <Select value={status} onChange={(e) => setStatus(e.target.value)} className="!w-auto">
              <option value="">Active & inactive</option>
              <option value="active">Active</option>
              <option value="inactive">Inactive</option>
              <option value="archived">Archived</option>
              <option value="all">All</option>
            </Select>
            <Select value={type} onChange={(e) => setType(e.target.value)} className="!w-auto">
              <option value="">Companies & individuals</option>
              <option value="company">Companies</option>
              <option value="individual">Individuals</option>
            </Select>
            <Select value={city} onChange={(e) => setCity(e.target.value)} className="!w-auto">
              <option value="">All cities</option>
              {(data?.cities || []).map(cy => <option key={cy} value={cy}>{cy}</option>)}
            </Select>
            {data && <span className="text-sm text-zinc-500">{data.total} client{data.total === 1 ? '' : 's'}</span>}
          </div>
          <ErrorText>{error}</ErrorText>
          {!data && !error ? <Spinner /> : data && (
            <>
              <Table head={['Client', 'Contact', 'City', 'NTN / CNIC', { label: 'Terms', right: true }, 'Status']}>
                {data.clients.length === 0 && (
                  <tr><td colSpan={6} className="px-3 py-6 text-center text-zinc-500">
                    {debounced || status || city || type ? 'No clients match.' : canManage ? 'No clients yet. Use "Add client".' : 'No clients yet.'}
                  </td></tr>
                )}
                {data.clients.map(c => (
                  <tr key={c.id} onClick={() => navigate(`clients/${c.id}`)}
                    className="border-t border-zinc-100 dark:border-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer">
                    <td className="px-3 py-2">
                      <div className="font-semibold flex items-center gap-1.5"><ClientTypeIcon type={c.client_type} className="w-3.5 h-3.5 text-zinc-400 shrink-0" /> {c.name}</div>
                      <div className="font-mono text-[11px] text-zinc-500">{c.client_number}</div>
                    </td>
                    <td className="px-3 py-2"><div>{c.client_type === 'individual' ? (c.contact_person ? `Alt: ${c.contact_person}` : 'Individual') : (c.contact_person || '—')}</div><div className="text-xs text-zinc-500">{[c.mobile || c.phone, c.email].filter(Boolean).join(' · ')}</div></td>
                    <td className="px-3 py-2 whitespace-nowrap">{c.city || '—'}</td>
                    <td className="px-3 py-2 whitespace-nowrap font-mono text-xs">{(c.client_type === 'individual' ? c.cnic || c.ntn : c.ntn || c.cnic) || '—'}</td>
                    <td className="px-3 py-2 text-right whitespace-nowrap">{c.terms_days} days</td>
                    <td className="px-3 py-2"><StatusBadge status={c.status} /></td>
                  </tr>
                ))}
              </Table>
              <Pager page={data.page} pageSize={data.page_size} total={data.total} onPage={setPage} />
            </>
          )}
        </>
      )}
      {form && (
        <ClientForm client={form.client} defaults={settings} onClose={() => setForm(null)}
          onSaved={(saved) => {
            const after = form.after;
            setForm(null);
            setReload(r => r + 1);
            if (after) after();
            else navigate(`clients/${saved.id}`);
          }} />
      )}
    </>
  );
};
