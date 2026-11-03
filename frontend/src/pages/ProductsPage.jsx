import React, { useEffect, useState } from 'react';
import { Package, Plus, Search, Pencil } from 'lucide-react';
import { api, qs } from '../lib/api';
import { useAuth, navigate } from '../lib/auth';
import { formatMoney, fmtDateTime, CHARGE_TYPES, CHARGE_SHORT } from '../lib/format';
import { TaxPicker } from '../components/TaxPicker';
import { useCompanies } from '../lib/companies';
import {
  PageHeader, Button, Field, Input, Select, Textarea, Badge, Modal, Table, Spinner, ErrorText, inputCls,
} from '../components/ui';

// Service catalogue: MPLS, IPT, DPLC, installation… with the usual rate.
// A client's own rate is set on their service, so changing a default rate
// here never changes what existing clients pay.

const EMPTY = { code: '', name: '', description: '', unit: '', charge_type: 'monthly', default_rate: '', active: true, tax_ids: [] };

const ProductForm = ({ product, onClose, onSaved }) => {
  const [form, setForm] = useState(() => (product
    ? { ...EMPTY, ...product, default_rate: String(product.default_rate ?? '') }
    : EMPTY));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [history, setHistory] = useState(null);
  const [taxes, setTaxes] = useState(null);
  const companies = useCompanies();
  const [applyMsg, setApplyMsg] = useState('');
  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.value }));

  useEffect(() => {
    if (product) api(`/api/products/${product.id}`).then(d => setHistory(d)).catch(() => {});
    api('/api/taxes?status=all').then(d => setTaxes(d.taxes)).catch(() => setTaxes([]));
  }, [product]);
  const taxesChanged = product && JSON.stringify([...(product.tax_ids || [])].sort()) !== JSON.stringify([...(form.tax_ids || [])].sort());

  // Put the product's (saved) taxes on all its current services.
  const applyToServices = async () => {
    if (!window.confirm(`Put this product's taxes on all ${history?.in_use || 0} of its current services? Invoices already made don't change.`)) return;
    setBusy(true);
    setError('');
    try {
      const d = await api(`/api/products/${product.id}/apply-taxes`, { method: 'POST', body: {} });
      setApplyMsg(`${d.changed} of ${d.services} service${d.services === 1 ? '' : 's'} changed.`);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const save = async () => {
    setError('');
    const rate = String(form.default_rate).trim() === '' ? 0 : Number(String(form.default_rate).replace(/,/g, ''));
    if (!form.name.trim()) { setError('Enter the product name'); return; }
    if (!Number.isFinite(rate) || rate < 0) { setError('Check the default rate'); return; }
    setBusy(true);
    try {
      const body = { ...form, default_rate: rate, tax_ids: form.tax_ids || [] };
      const d = product
        ? await api(`/api/products/${product.id}`, { method: 'PUT', body })
        : await api('/api/products', { method: 'POST', body });
      onSaved(d.product);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title={product ? `Edit ${product.name}` : 'Add product'} onClose={onClose}
      footer={<>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button busy={busy} onClick={save}>{product ? 'Save changes' : 'Add product'}</Button>
      </>}>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <Field label="Name" required className="sm:col-span-2" hint="Printed on invoices, e.g. MPLS 10 Mbps.">
          <Input autoFocus value={form.name} onChange={set('name')} maxLength={200} />
        </Field>
        <Field label="Code" hint="Optional short code.">
          <Input value={form.code} onChange={set('code')} maxLength={30} placeholder="MPLS-10M" />
        </Field>
        <Field label="Charge type" required>
          <Select value={form.charge_type} onChange={set('charge_type')}>
            {Object.entries(CHARGE_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
          </Select>
        </Field>
        <Field label="Default rate (PKR)" hint={`Per unit, ${CHARGE_SHORT[form.charge_type]}, before tax.`}>
          <Input inputMode="decimal" value={form.default_rate} onChange={set('default_rate')} maxLength={20} placeholder="0" />
        </Field>
        <Field label="Unit" hint="Optional: link, Mbps, line…">
          <Input value={form.unit} onChange={set('unit')} maxLength={40} />
        </Field>
        <Field label="Taxes" className="sm:col-span-3" hint={companies.length > 1 ? 'New services of this product get the ones of their billing company (each service can change its own).' : 'New services of this product get these taxes (each service can change its own).'}>
          {!taxes ? <Spinner /> : <TaxPicker taxes={taxes} companies={companies} value={form.tax_ids} onChange={(ids) => setForm(f => ({ ...f, tax_ids: ids }))} />}
          {product && history?.in_use > 0 && (
            <div className="mt-2 text-xs flex flex-wrap items-center gap-2">
              {taxesChanged
                ? <span className="text-zinc-500">Save first, then you can put these taxes on its {history.in_use} current service{history.in_use === 1 ? '' : 's'}.</span>
                : <Button variant="secondary" busy={busy} onClick={applyToServices}>Put these taxes on its {history.in_use} current service{history.in_use === 1 ? '' : 's'}</Button>}
              {applyMsg && <span className="text-emerald-600">{applyMsg}</span>}
            </div>
          )}
        </Field>
        <Field label="Description" className="sm:col-span-3">
          <Textarea rows={2} value={form.description} onChange={set('description')} maxLength={2000} />
        </Field>
        {product && (
          <Field label="Status" className="sm:col-span-3"
            hint={history?.in_use ? `${history.in_use} client service(s) use this. Making it inactive only stops new ones.` : 'Inactive products can\'t be added to clients.'}>
            <Select value={form.active ? '1' : '0'} onChange={(e) => setForm(f => ({ ...f, active: e.target.value === '1' }))}>
              <option value="1">Active (on sale)</option>
              <option value="0">Inactive</option>
            </Select>
          </Field>
        )}
      </div>
      <ErrorText>{error}</ErrorText>
      {history?.history?.length > 0 && (
        <details className="mt-4">
          <summary className="text-xs font-semibold text-zinc-500 cursor-pointer">History</summary>
          <ol className="mt-2 space-y-1">
            {history.history.map(h => (
              <li key={h.id} className="text-xs text-zinc-600 dark:text-zinc-400">
                {fmtDateTime(h.created_at)} · <b>{h.user_name}</b> · {h.action} · {h.details}
              </li>
            ))}
          </ol>
        </details>
      )}
    </Modal>
  );
};

export const ProductsPage = () => {
  const { can } = useAuth();
  const canManage = can('manage_billing');
  const [q, setQ] = useState('');
  const [status, setStatus] = useState('active');
  const [type, setType] = useState('');
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [form, setForm] = useState(null); // null | { product? }
  const [reload, setReload] = useState(0);

  useEffect(() => {
    const ctrl = new AbortController();
    const t = setTimeout(() => {
      setError('');
      api(`/api/products${qs({ q, status, charge_type: type })}`, { signal: ctrl.signal })
        .then(setData).catch(err => { if (err.name !== 'AbortError') setError(err.message); });
    }, 250);
    return () => { clearTimeout(t); ctrl.abort(); };
  }, [q, status, type, reload]);

  return (
    <>
      <PageHeader icon={Package} title="Products" subtitle="The services you sell, with their usual rate."
        actions={canManage && <Button icon={Plus} onClick={() => setForm({})}>Add product</Button>} />
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="relative flex-1 min-w-[14rem] max-w-md">
          <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Name or code…" className={`${inputCls} pl-8`} />
        </span>
        <Select value={type} onChange={(e) => setType(e.target.value)} className="!w-auto">
          <option value="">All charge types</option>
          {Object.entries(CHARGE_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </Select>
        <Select value={status} onChange={(e) => setStatus(e.target.value)} className="!w-auto">
          <option value="active">On sale</option>
          <option value="inactive">Inactive</option>
          <option value="all">All</option>
        </Select>
        {data && <span className="text-sm text-zinc-500">{data.products.length} product{data.products.length === 1 ? '' : 's'}</span>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data && !error ? <Spinner /> : data && (
        <Table head={['Product', 'Charge type', { label: 'Default rate', right: true }, { label: 'Clients using', right: true }, 'Status', '']}>
          {data.products.length === 0 && (
            <tr><td colSpan={6} className="px-3 py-6 text-center text-zinc-500">
              {q || type || status !== 'active' ? 'No products match.' : canManage ? 'No products yet. Use "Add product" (e.g. MPLS, IPT, DPLC, Installation).' : 'No products yet.'}
            </td></tr>
          )}
          {data.products.map(p => (
            <tr key={p.id} className="border-t border-zinc-100 dark:border-zinc-800">
              <td className="px-3 py-2">
                <div className="font-semibold">{p.name}</div>
                <div className="text-[11px] text-zinc-500">{[p.code, p.unit && `per ${p.unit}`].filter(Boolean).join(' · ')}</div>
              </td>
              <td className="px-3 py-2 whitespace-nowrap">{CHARGE_TYPES[p.charge_type]}</td>
              <td className="px-3 py-2 text-right whitespace-nowrap">{formatMoney(p.default_rate)} <span className="text-xs text-zinc-500">{CHARGE_SHORT[p.charge_type]}</span></td>
              <td className="px-3 py-2 text-right">
                {data.in_use?.[p.id]
                  ? <button type="button" className="text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer" onClick={() => navigate(`services?product=${p.id}`)}>{data.in_use[p.id]}</button>
                  : <span className="text-zinc-400">0</span>}
              </td>
              <td className="px-3 py-2">{p.active ? <Badge tone="green">On sale</Badge> : <Badge>Inactive</Badge>}</td>
              <td className="px-3 py-2 text-right">
                {canManage && <Button variant="ghost" icon={Pencil} onClick={() => setForm({ product: p })} aria-label={`Edit ${p.name}`} />}
              </td>
            </tr>
          ))}
        </Table>
      )}
      {form && (
        <ProductForm product={form.product} onClose={() => setForm(null)}
          onSaved={() => { setForm(null); setReload(r => r + 1); }} />
      )}
    </>
  );
};
