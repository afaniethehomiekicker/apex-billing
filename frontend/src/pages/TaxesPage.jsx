import React, { useEffect, useState } from 'react';
import { Percent, Plus, Pencil, History } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth } from '../lib/auth';
import { fmtDateTime, pctText, TAX_KINDS, invoiceTaxes, formatMoney } from '../lib/format';
import {
  PageHeader, Button, Field, Input, Select, Textarea, Badge, Modal, Table, Spinner, ErrorText, Card,
} from '../components/ui';
import { CompanySelect } from '../components/CompanySelect';
import { useCompanies, defaultCompany, companyName } from '../lib/companies';

// Taxes, set up by hand (as many as needed): sales tax per province, FED,
// withholding shown on the invoice… Products and services choose theirs;
// each invoice line keeps the rate it was billed at. Each company has its own
// taxes: a service or invoice only uses its billing company's.

const EMPTY = { name: '', short_name: '', rate: '', kind: 'add', compound: false, order: '10', description: '', active: true };

// A worked example on 10,000 with this tax and the others before it.
const Example = ({ form, taxes, editingId }) => {
  const rate = Number(String(form.rate).replace(/,/g, ''));
  if (!Number.isFinite(rate) || rate <= 0) return null;
  const me = { id: editingId || -1, name: form.short_name || form.name || 'This tax', rate, kind: form.kind, compound: form.compound, order: Number(form.order) || 0 };
  // Compound taxes are worked out on top of the added taxes before them:
  // show it with the active added taxes that come first.
  const before = form.compound ? (taxes || []).filter(t => t.active && t.kind === 'add' && t.id !== editingId && (t.order < me.order || (t.order === me.order && t.id < me.id))) : [];
  const r = invoiceTaxes([{ amount: 10000, taxes: [...before, me] }]);
  return (
    <div className="sm:col-span-3 p-3 rounded-lg bg-indigo-50/60 dark:bg-indigo-950/20 border border-indigo-200 dark:border-indigo-900 text-sm">
      <div className="font-semibold mb-1">Example: a line of {formatMoney(10000)}</div>
      {r.taxLines.map((t, i) => (
        <div key={i} className="flex justify-between gap-4"><span>{t.kind === 'deduct' ? 'Less: ' : ''}{t.short_name || t.name} {pctText(t.rate)} on {formatMoney(t.base)}</span><span>{t.kind === 'deduct' ? '−' : ''}{formatMoney(t.amount)}</span></div>
      ))}
      <div className="flex justify-between gap-4 font-semibold border-t border-indigo-200 dark:border-indigo-900 mt-1 pt-1"><span>Total</span><span>{formatMoney(r.total)}</span></div>
      {form.compound && before.length === 0 && <div className="text-[11px] text-zinc-500 mt-1">Compound: no added tax comes before this one (lower order), so it works like a normal tax.</div>}
    </div>
  );
};

const TaxForm = ({ tax, taxes: allTaxes, companyId: presetCompany, onClose, onSaved }) => {
  const companies = useCompanies();
  const [form, setForm] = useState(() => (tax ? { ...EMPTY, ...tax, rate: String(Number(tax.rate)), order: String(tax.order) } : { ...EMPTY, company_id: presetCompany || '' }));
  const coId = form.company_id || defaultCompany(companies)?.id || '';
  // The worked example uses this company's other taxes.
  const taxes = (allTaxes || []).filter(t => !coId || t.company_id === Number(coId));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [history, setHistory] = useState(null);
  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));

  useEffect(() => { if (tax) api(`/api/taxes/${tax.id}`).then(d => setHistory(d.history)).catch(() => {}); }, [tax]);

  const save = async () => {
    setError('');
    const rate = Number(String(form.rate).replace(/,/g, '').trim());
    const order = Number(String(form.order).trim() || '0');
    if (!form.name.trim()) { setError('Enter the tax name'); return; }
    if (!Number.isFinite(rate) || rate < 0 || rate > 100) { setError('Rate must be between 0 and 100%'); return; }
    if (!Number.isInteger(order) || order < 0) { setError('Order must be a whole number'); return; }
    setBusy(true);
    try {
      const body = { ...form, company_id: Number(coId) || 0, rate, order, active: !!form.active };
      const d = tax
        ? await api(`/api/taxes/${tax.id}`, { method: 'PUT', body })
        : await api('/api/taxes', { method: 'POST', body });
      onSaved(d.tax);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal wide title={tax ? `Edit ${tax.name}` : 'Add tax'} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} onClick={save}>{tax ? 'Save changes' : 'Add tax'}</Button></>}>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        {companies.length > 1 && (
          <Field label="Company" required className="sm:col-span-3" hint={tax ? 'A tax stays with its company. For another company, add a new tax.' : 'The company that charges this tax. Only its services and invoices can use it.'}>
            {tax
              ? <div className="text-sm py-2">{companyName(companies, tax.company_id)}</div>
              : <CompanySelect companies={companies} value={coId} onChange={(v) => setForm(f => ({ ...f, company_id: v }))} />}
          </Field>
        )}
        <Field label="Name" required className="sm:col-span-2" hint="Printed on invoices, e.g. Punjab Sales Tax on Services.">
          <Input autoFocus value={form.name} onChange={set('name')} maxLength={100} />
        </Field>
        <Field label="Short name" hint="Shown on invoice lines, e.g. PST.">
          <Input value={form.short_name} onChange={set('short_name')} maxLength={30} />
        </Field>
        <Field label="Rate %" required>
          <Input inputMode="decimal" value={form.rate} onChange={set('rate')} maxLength={8} placeholder="16" />
        </Field>
        <Field label="Added or deducted" hint={form.kind === 'deduct' ? 'Taken off the invoice total (shown as "Less:").' : 'Added to the invoice total.'}>
          <Select value={form.kind} onChange={set('kind')}>
            <option value="add">Added to the invoice</option>
            <option value="deduct">Deducted from the invoice</option>
          </Select>
        </Field>
        <Field label="Order" hint="Lower first. Decides the order taxes are worked out and printed.">
          <Input inputMode="numeric" value={form.order} onChange={set('order')} maxLength={3} />
        </Field>
        <label className="sm:col-span-3 flex items-start gap-2 text-sm">
          <input type="checkbox" className="mt-1" checked={!!form.compound} onChange={set('compound')} />
          <span><b>Tax on tax (compound)</b>: work this out on the amount <i>plus</i> the added taxes that come before it (lower order), not on the amount alone.</span>
        </label>
        <Example form={form} taxes={taxes} editingId={tax?.id} />
        <Field label="Notes" className="sm:col-span-3"><Textarea rows={2} value={form.description} onChange={set('description')} maxLength={2000} /></Field>
        {tax && (
          <label className="sm:col-span-3 flex items-center gap-2 text-sm">
            <input type="checkbox" checked={!!form.active} onChange={set('active')} /> Active (inactive taxes can't be chosen again; products and services that have it keep it)
          </label>
        )}
        {tax && Number(form.rate) !== Number(tax.rate) && (
          <p className="sm:col-span-3 text-xs text-amber-700 dark:text-amber-400">A new rate is used for charges billed from now on. Issued invoices keep the rate they were billed at; open drafts change when next edited or when charges are added.</p>
        )}
      </div>
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
      {history?.length > 0 && (
        <details className="mt-4">
          <summary className="text-xs font-semibold text-zinc-500 cursor-pointer flex items-center gap-1"><History className="w-3.5 h-3.5" /> History</summary>
          <ol className="mt-2 space-y-1">
            {history.map(h => <li key={h.id} className="text-xs text-zinc-600 dark:text-zinc-400">{fmtDateTime(h.created_at)} · <b>{h.user_name}</b> · {h.action} · {h.details}</li>)}
          </ol>
        </details>
      )}
    </Modal>
  );
};

export const TaxesPage = () => {
  const { can } = useAuth();
  const canManage = can('manage_billing');
  const [status, setStatus] = useState('active');
  const [data, setData] = useState(null);
  const [all, setAll] = useState([]);
  const [error, setError] = useState('');
  const [form, setForm] = useState(null);
  const [reload, setReload] = useState(0);
  const [companyId, setCompanyId] = useState('');
  const companies = useCompanies();
  const many = companies.length > 1;

  useEffect(() => {
    setError('');
    api(`/api/taxes?status=${status}${companyId ? `&company_id=${companyId}` : ''}`).then(setData).catch(err => setError(err.message));
    api('/api/taxes?status=all').then(d => setAll(d.taxes)).catch(() => {});
  }, [status, companyId, reload]);

  return (
    <>
      <PageHeader icon={Percent} title="Taxes" subtitle="Your taxes, as many as you need. Choose them on each product and service."
        actions={canManage && <Button icon={Plus} onClick={() => setForm({})}>Add tax</Button>} />
      <div className="flex flex-wrap items-center gap-2 mb-3">
        {many && <CompanySelect companies={companies} value={companyId} allLabel="All companies" onChange={setCompanyId} className="!w-auto" />}
        <Select value={status} onChange={(e) => setStatus(e.target.value)} className="!w-auto">
          <option value="active">Active</option>
          <option value="inactive">Inactive</option>
          <option value="all">All</option>
        </Select>
        {data && <span className="text-sm text-zinc-500">{data.taxes.length} tax{data.taxes.length === 1 ? '' : 'es'}</span>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data ? (!error && <Spinner />) : data.taxes.length === 0 ? (
        <Card className="p-6 text-sm text-zinc-500 text-center">
          {status === 'active' ? (canManage ? 'No taxes yet. Add each one you charge (e.g. "Punjab Sales Tax on Services 16%"), then choose them on your products.' : 'No taxes yet.') : 'None.'}
        </Card>
      ) : (
        <Table head={['Order', 'Tax', ...(many ? ['Company'] : []), { label: 'Rate', right: true }, 'Type', { label: 'Products', right: true }, { label: 'Services', right: true }, '']}>
          {data.taxes.map(t => (
            <tr key={t.id} className="border-t border-zinc-100 dark:border-zinc-800">
              <td className="px-3 py-2 text-zinc-500">{t.order}</td>
              <td className="px-3 py-2 min-w-[12rem]">
                <div className="font-semibold flex items-center gap-2">{t.name} {!t.active && <Badge>Inactive</Badge>}</div>
                {(t.short_name || t.description) && <div className="text-[11px] text-zinc-500">{[t.short_name, t.description].filter(Boolean).join(' · ')}</div>}
              </td>
              {many && <td className="px-3 py-2 whitespace-nowrap">{companyName(companies, t.company_id)}</td>}
              <td className="px-3 py-2 text-right font-semibold whitespace-nowrap">{pctText(t.rate)}</td>
              <td className="px-3 py-2 whitespace-nowrap">
                <Badge tone={t.kind === 'deduct' ? 'amber' : 'indigo'}>{TAX_KINDS[t.kind]}</Badge>
                {t.compound && <span className="ml-1"><Badge>Compound</Badge></span>}
              </td>
              <td className="px-3 py-2 text-right">{data.products[t.id] || 0}</td>
              <td className="px-3 py-2 text-right">{data.services[t.id] || 0}</td>
              <td className="px-3 py-2 text-right">{canManage && <Button variant="ghost" icon={Pencil} onClick={() => setForm({ tax: t })} aria-label={`Edit ${t.name}`} />}</td>
            </tr>
          ))}
        </Table>
      )}
      <p className="text-[11px] text-zinc-500 mt-2">Each tax is worked out per invoice line on the line amount; a compound tax on the amount plus the added taxes before it. Deducted taxes reduce the invoice total.</p>
      {form && <TaxForm tax={form.tax} taxes={all} companyId={companyId} onClose={() => setForm(null)} onSaved={() => { setForm(null); setReload(r => r + 1); }} />}
    </>
  );
};
