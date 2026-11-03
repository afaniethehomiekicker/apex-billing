import React, { useEffect, useRef, useState } from 'react';
import { Settings, Save, Plus, Building2, Upload, Trash2, Star, History, ImageOff } from 'lucide-react';
import { api } from '../lib/api';
import { navigate } from '../lib/auth';
import { reloadCompanies } from '../lib/companies';
import { fmtDateTime } from '../lib/format';
import { PageHeader, Button, Field, Input, Textarea, Card, Spinner, ErrorText, Badge } from '../components/ui';
import { CompanyLogo } from '../components/CompanyLogo';

// Company settings (admins): our companies — each with its own details,
// logo, bank details and invoice numbers, printed on its invoices — and the
// general settings (defaults for new clients, security).
// #/settings opens the default company; #/settings/3 another; #/settings/new adds one.

const EMPTY = {
  name: '', address: '', phone: '', email: '', website: '', ntn: '', strn: '', bank_details: '', invoice_footer: '',
  invoice_prefix: '', is_default: false, active: true,
};

const logoVersion = (co) => `${co.logo_name}|${co.logo_size}|${co.updated_at}`;

// ---- one company: details, invoices, logo ----
const CompanyEditor = ({ company, onSaved }) => {
  const isNew = !company;
  const [form, setForm] = useState(() => ({ ...EMPTY, ...(company || {}) }));
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [saved, setSaved] = useState('');
  const [history, setHistory] = useState(null);
  const fileRef = useRef(null);

  useEffect(() => {
    setForm({ ...EMPTY, ...(company || {}) });
    setError(''); setSaved(''); setHistory(null);
  }, [company?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const set = (k) => (e) => { setSaved(''); setForm(f => ({ ...f, [k]: e.target.value })); };
  const setBool = (k) => (e) => { setSaved(''); setForm(f => ({ ...f, [k]: e.target.checked })); };

  const save = async () => {
    setError(''); setBusy('save');
    try {
      const body = { ...form, invoice_prefix: form.invoice_prefix.toUpperCase() };
      const d = isNew
        ? await api('/api/companies', { method: 'POST', body })
        : await api(`/api/companies/${company.id}`, { method: 'PUT', body });
      await reloadCompanies().catch(() => {});
      setSaved(isNew ? 'Company added.' : 'Saved.');
      onSaved(d.company, isNew);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy('');
    }
  };

  const uploadLogo = async (file) => {
    if (!file) return;
    setError(''); setSaved('');
    if (file.size > 2 * 1024 * 1024) { setError('The logo is too big (max 2 MB).'); return; }
    setBusy('logo');
    try {
      const fd = new FormData();
      fd.append('file', file);
      const d = await api(`/api/companies/${company.id}/logo`, { method: 'POST', form: fd });
      await reloadCompanies().catch(() => {});
      setSaved('Logo saved. New invoices will print it.');
      onSaved(d.company, false);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy('');
      if (fileRef.current) fileRef.current.value = '';
    }
  };

  const removeLogo = async () => {
    if (!window.confirm('Remove the logo? Invoices made from now on print without it. Invoices already issued keep theirs.')) return;
    setError(''); setBusy('logo');
    try {
      const d = await api(`/api/companies/${company.id}/logo/remove`, { method: 'POST', body: {} });
      await reloadCompanies().catch(() => {});
      setSaved('Logo removed.');
      onSaved(d.company, false);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy('');
    }
  };

  const showHistory = () => {
    api(`/api/companies/${company.id}`).then(d => setHistory(d.history || [])).catch(err => setError(err.message));
  };

  const prefix = (form.invoice_prefix || 'INV').toUpperCase();
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <h2 className="text-base font-semibold truncate">{isNew ? 'New company' : company.name}</h2>
          {company?.is_default && <Badge tone="indigo">Default</Badge>}
          {company && !company.active && <Badge>Inactive</Badge>}
        </div>
        <div className="flex gap-2">
          {!isNew && <Button variant="ghost" icon={History} onClick={showHistory}>History</Button>}
          <Button icon={isNew ? Plus : Save} busy={busy === 'save'} onClick={save}>{isNew ? 'Add company' : 'Save'}</Button>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-5">
        <Card className="p-5 space-y-3">
          <h3 className="text-sm font-semibold">Company</h3>
          <Field label="Company name" required><Input value={form.name} onChange={set('name')} maxLength={200} /></Field>
          <Field label="Address"><Textarea rows={3} value={form.address} onChange={set('address')} maxLength={1000} /></Field>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="Phone"><Input value={form.phone} onChange={set('phone')} maxLength={60} /></Field>
            <Field label="Email"><Input value={form.email} onChange={set('email')} maxLength={200} /></Field>
            <Field label="NTN"><Input value={form.ntn} onChange={set('ntn')} maxLength={40} /></Field>
            <Field label="STRN"><Input value={form.strn} onChange={set('strn')} maxLength={40} /></Field>
          </div>
          <Field label="Website"><Input value={form.website} onChange={set('website')} maxLength={200} /></Field>
        </Card>

        <div className="space-y-5">
          <Card className="p-5 space-y-3">
            <h3 className="text-sm font-semibold">Logo</h3>
            {isNew ? (
              <p className="text-sm text-zinc-500">Add the company first, then upload its logo.</p>
            ) : (
              <div className="flex flex-wrap items-center gap-4">
                <div className="w-48 h-24 rounded-lg border border-dashed border-zinc-300 dark:border-zinc-700 bg-white flex items-center justify-center overflow-hidden">
                  {company.has_logo
                    ? <CompanyLogo src={`/api/companies/${company.id}/logo`} version={logoVersion(company)} className="max-w-full max-h-full object-contain p-2" />
                    : <span className="flex flex-col items-center gap-1 text-xs text-zinc-400"><ImageOff className="w-5 h-5" />No logo</span>}
                </div>
                <div className="space-y-2">
                  <input ref={fileRef} type="file" accept="image/png,image/jpeg,image/webp" className="hidden"
                    onChange={(e) => uploadLogo(e.target.files?.[0])} />
                  <div className="flex flex-wrap gap-2">
                    <Button variant="secondary" icon={Upload} busy={busy === 'logo'} onClick={() => fileRef.current?.click()}>
                      {company.has_logo ? 'Replace logo' : 'Upload logo'}
                    </Button>
                    {company.has_logo && <Button variant="ghost" icon={Trash2} disabled={!!busy} onClick={removeLogo}>Remove</Button>}
                  </div>
                  <p className="text-[11px] text-zinc-500 max-w-xs">PNG, JPG or WEBP, up to 2 MB. A wide logo on a white or clear background prints best. Printed at the top of this company's invoices.</p>
                </div>
              </div>
            )}
          </Card>

          <Card className="p-5 space-y-3">
            <h3 className="text-sm font-semibold">Invoices</h3>
            <Field label="Bank / payment details" hint="Printed at the bottom of this company's invoices (bank, account title, IBAN…).">
              <Textarea rows={4} value={form.bank_details} onChange={set('bank_details')} maxLength={2000} />
            </Field>
            <Field label="Invoice footer" hint="E.g. terms, a thank-you note.">
              <Textarea rows={2} value={form.invoice_footer} onChange={set('invoice_footer')} maxLength={2000} />
            </Field>
            <Field label="Invoice number prefix" required
              hint={`This company's invoices will be ${prefix}-000001, ${prefix}-000002… Each company needs its own prefix (letters and numbers, up to 6).`}>
              <Input value={form.invoice_prefix} onChange={set('invoice_prefix')} maxLength={6} placeholder="e.g. INV" className="uppercase sm:max-w-[12rem]" />
            </Field>
          </Card>
        </div>
      </div>

      <Card className="p-5 space-y-2">
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" className="mt-1" checked={!!form.is_default} disabled={!!company?.is_default} onChange={setBool('is_default')} />
          <span><b>Default company</b><br />
            <span className="text-xs text-zinc-500">{company?.is_default
              ? 'New services are billed by this company unless you choose another. To change, make another company the default.'
              : 'New services are billed by the default company unless you choose another.'}</span></span>
        </label>
        {!isNew && (
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1" checked={!!form.active} disabled={!!company?.is_default} onChange={setBool('active')} />
            <span><b>Active</b><br />
              <span className="text-xs text-zinc-500">An inactive company can't be chosen for new services or invoices. Its issued invoices stay as they are.</span></span>
          </label>
        )}
      </Card>

      <div><ErrorText>{error}</ErrorText>{saved && <p className="text-sm text-emerald-700 dark:text-emerald-400">{saved}</p>}</div>

      {history && (
        <Card className="p-5">
          <h3 className="text-sm font-semibold mb-2">History</h3>
          {history.length === 0 ? <p className="text-sm text-zinc-500">No changes recorded.</p> : (
            <ul className="space-y-1.5 text-sm">
              {history.map(h => (
                <li key={h.id}><span className="text-zinc-500">{fmtDateTime(h.created_at)} · {h.user_name}:</span> {h.details || h.action}</li>
              ))}
            </ul>
          )}
        </Card>
      )}
    </div>
  );
};

// ---- general settings ----
const GeneralSettings = () => {
  const [form, setForm] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api('/api/settings').then(d => setForm({
      currency: d.settings.currency || 'PKR',
      default_terms_days: String(d.settings.default_terms_days ?? 15),
      default_tax_percent: d.settings.default_tax_percent ?? 0,
      require_2fa: !!d.settings.require_2fa,
    })).catch(err => setError(err.message));
  }, []);
  const set = (k) => (e) => { setSaved(false); setForm(f => ({ ...f, [k]: e.target.value })); };

  const save = async () => {
    setError(''); setBusy(true);
    try {
      await api('/api/settings', { method: 'PUT', body: {
        ...form, default_terms_days: Math.round(Number(form.default_terms_days || 0)), default_tax_percent: Number(form.default_tax_percent || 0),
      } });
      setSaved(true);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  if (!form) return error ? <ErrorText>{error}</ErrorText> : <Spinner />;
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
      <Card className="p-5 space-y-3">
        <h3 className="text-sm font-semibold">Defaults (all companies)</h3>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="Currency"><Input value={form.currency} onChange={set('currency')} maxLength={10} /></Field>
          <Field label="Default payment terms (days)" hint="For new clients.">
            <Input inputMode="numeric" value={form.default_terms_days} onChange={set('default_terms_days')} maxLength={3} />
          </Field>
        </div>
      </Card>
      <Card className="p-5 space-y-2">
        <h3 className="text-sm font-semibold">Security</h3>
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" className="mt-1" checked={!!form.require_2fa} onChange={(e) => { setSaved(false); setForm(f => ({ ...f, require_2fa: e.target.checked })); }} />
          <span><b>Require two-step sign-in for everyone</b><br />
            <span className="text-xs text-zinc-500">Each user must set up an authenticator app on their phone at their next sign-in before they can do anything else. Strongly recommended. Set up your own first (My account).</span></span>
        </label>
      </Card>
      <div className="lg:col-span-2 flex flex-wrap items-center gap-3">
        <Button icon={Save} variant="secondary" busy={busy} onClick={save}>Save general settings</Button>
        <ErrorText>{error}</ErrorText>{saved && <span className="text-sm text-emerald-700 dark:text-emerald-400">Saved.</span>}
      </div>
    </div>
  );
};

export const SettingsPage = ({ arg }) => {
  const [companies, setCompanies] = useState(null);
  const [error, setError] = useState('');

  const load = () => api('/api/companies?status=all').then(d => setCompanies(d.companies)).catch(err => setError(err.message));
  useEffect(() => { load(); }, []);

  if (!companies) return error ? <ErrorText>{error}</ErrorText> : <Spinner />;
  const adding = arg === 'new';
  const selected = adding ? null : (companies.find(c => String(c.id) === arg) || companies.find(c => c.is_default) || companies[0]);

  const onSaved = (co, isNew) => {
    load();
    if (isNew) navigate(`settings/${co.id}`);
  };

  return (
    <>
      <PageHeader icon={Settings} title="Company settings"
        subtitle="Your companies — each with its own logo, details, bank details and invoice numbers, printed on its invoices." />
      <div className="grid grid-cols-1 lg:grid-cols-[16rem_1fr] gap-5 items-start">
        <Card className="p-2">
          <div className="px-2 pt-1 pb-2 text-xs font-semibold uppercase tracking-wider text-zinc-500">Companies</div>
          <div className="space-y-1">
            {companies.map(c => {
              const on = !adding && selected?.id === c.id;
              return (
                <button key={c.id} type="button" onClick={() => navigate(`settings/${c.id}`)}
                  className={`w-full flex items-center gap-2.5 p-2 rounded-lg text-left cursor-pointer ${on ? 'bg-indigo-50 dark:bg-indigo-950/40 ring-1 ring-indigo-300 dark:ring-indigo-800' : 'hover:bg-zinc-100 dark:hover:bg-zinc-800/60'}`}>
                  <div className="w-10 h-10 shrink-0 rounded-md bg-white border border-zinc-200 dark:border-zinc-700 flex items-center justify-center overflow-hidden">
                    {c.has_logo
                      ? <CompanyLogo src={`/api/companies/${c.id}/logo`} version={logoVersion(c)} className="max-w-full max-h-full object-contain p-0.5" />
                      : <Building2 className="w-4 h-4 text-zinc-400" />}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className={`text-sm font-medium truncate ${c.active ? '' : 'text-zinc-400'}`}>{c.name}</div>
                    <div className="text-[11px] text-zinc-500 flex items-center gap-1.5">
                      <span className="font-mono">{c.invoice_prefix}-</span>
                      <span>· {c.services} service{c.services === 1 ? '' : 's'}</span>
                      {c.is_default && <Star className="w-3 h-3 text-indigo-500 fill-indigo-500" aria-label="Default" />}
                      {!c.active && <span>· inactive</span>}
                    </div>
                  </div>
                </button>
              );
            })}
          </div>
          <div className="p-1 pt-2">
            <Button variant={adding ? 'primary' : 'secondary'} icon={Plus} className="w-full" onClick={() => navigate('settings/new')}>Add company</Button>
          </div>
        </Card>

        <CompanyEditor key={adding ? 'new' : selected?.id} company={selected} onSaved={onSaved} />
      </div>

      <h2 className="text-base font-semibold mt-8 mb-3">General settings</h2>
      <GeneralSettings />
    </>
  );
};
