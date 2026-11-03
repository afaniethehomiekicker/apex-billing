import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Network, Plus, Search, Pencil, PauseCircle, PlayCircle, CalendarX, History, ShieldCheck, Calculator } from 'lucide-react';
import { api, qs } from '../lib/api';
import { useAuth, navigate } from '../lib/auth';
import {
  formatMoney, fmtDay, fmtDateTime, dayOf, todayISO, CHARGE_TYPES, CHARGE_SHORT, SERVICE_STATES, partMonths,
  slaTargets, slaCredit, daysInMonth, pctText, minutesText, taxShort,
} from '../lib/format';
import { CreditSteps } from './SlaPage';
import { TaxPicker } from '../components/TaxPicker';
import { CompanySelect } from '../components/CompanySelect';
import { useCompanies, defaultCompany } from '../lib/companies';
import {
  PageHeader, Button, Field, Input, Select, Textarea, Badge, Modal, Pager, Table, Spinner, ErrorText, Card, inputCls,
} from '../components/ui';

// Client services (subscriptions): what each client takes, at what rate,
// from when to when. #/services lists them all; #/services/12 opens one;
// each client's page shows its own (ClientServices below).

export const StateBadge = ({ state }) => {
  const [label, tone] = SERVICE_STATES[state] || [state, 'zinc'];
  return <Badge tone={tone}>{label}</Badge>;
};

const num = (v) => (String(v ?? '').trim() === '' ? NaN : Number(String(v).replace(/,/g, '')));
const qtyText = (q) => (Number(q) === 1 ? '' : `${Number(q)} × `);

const period = (s) => {
  if (s.charge_type === 'one_time') return fmtDay(s.start_date);
  return `${fmtDay(s.start_date)} → ${s.end_date ? fmtDay(s.end_date) : 'ongoing'}`;
};

// ---- choose a client (search as you type) ----
export const ClientPicker = ({ value, onChange }) => {
  const [q, setQ] = useState('');
  const [list, setList] = useState([]);
  const [open, setOpen] = useState(false);
  const box = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    const ctrl = new AbortController();
    const t = setTimeout(() => {
      api(`/api/clients${qs({ q, page_size: 8 })}`, { signal: ctrl.signal }).then(d => setList(d.clients)).catch(() => {});
    }, 200);
    return () => { clearTimeout(t); ctrl.abort(); };
  }, [q, open]);
  useEffect(() => {
    const close = (e) => { if (box.current && !box.current.contains(e.target)) setOpen(false); };
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, []);

  if (value) {
    return (
      <div className={`${inputCls} flex items-center justify-between gap-2`}>
        <span className="truncate"><b>{value.name}</b> <span className="font-mono text-xs text-zinc-500">{value.client_number}</span></span>
        <button type="button" className="text-xs text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer" onClick={() => { onChange(null); setOpen(true); }}>Change</button>
      </div>
    );
  }
  return (
    <div className="relative" ref={box}>
      <input autoFocus value={q} onChange={(e) => { setQ(e.target.value); setOpen(true); }} onFocus={() => setOpen(true)}
        placeholder="Type the client's name or number…" className={inputCls} />
      {open && (
        <div className="absolute z-10 mt-1 w-full max-h-64 overflow-y-auto rounded-lg border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 shadow-lg">
          {list.length === 0 && <div className="px-3 py-2 text-sm text-zinc-500">No clients found.</div>}
          {list.map(c => (
            <button key={c.id} type="button" onClick={() => { onChange(c); setOpen(false); setQ(''); }}
              className="w-full text-left px-3 py-2 text-sm hover:bg-zinc-100 dark:hover:bg-zinc-800 cursor-pointer">
              <b>{c.name}</b> <span className="font-mono text-xs text-zinc-500">{c.client_number}</span>
              {c.city && <span className="text-xs text-zinc-500"> · {c.city}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

// ---- what the first / last invoice will charge ----
const BillingPreview = ({ form }) => {
  const amount = num(form.quantity) * num(form.rate);
  if (!Number.isFinite(amount) || !form.start_date) return null;
  let lines;
  if (form.charge_type === 'one_time') {
    lines = [`Charged once, ${formatMoney(amount)}, on the first invoice made on or after ${fmtDay(form.start_date)}.`];
  } else if (form.charge_type === 'annual') {
    lines = [`${formatMoney(amount)} a year, billed once a year from ${fmtDay(form.start_date)}.`];
  } else {
    lines = [`${formatMoney(amount)} a month, billed in advance.`];
    const from = form.bill_from && form.bill_from > form.start_date ? form.bill_from : form.start_date;
    partMonths(amount, from, form.end_date).forEach(p => {
      const which = p.which === 'last' ? 'Last month' : p.which === 'only' ? 'Only month' : 'First month';
      lines.push(`${which}, ${p.label}: ${p.days} of ${p.of} days → ${formatMoney(p.amount)}`);
    });
  }
  return (
    <div className="sm:col-span-3 p-3 rounded-lg bg-indigo-50/60 dark:bg-indigo-950/20 border border-indigo-200 dark:border-indigo-900 text-sm space-y-0.5">
      {lines.map((l, i) => <div key={i} className={i === 0 ? 'font-semibold' : 'text-zinc-700 dark:text-zinc-300'}>{l}</div>)}
      <div className="text-[11px] text-zinc-500">Before tax. The service's taxes are worked out on the invoice.</div>
    </div>
  );
};

const fromService = (s) => ({
  company_id: s.company_id ? String(s.company_id) : '',
  product_id: String(s.product_id), description: s.description || '', reference: s.reference || '',
  charge_type: s.charge_type, quantity: String(Number(s.quantity)), rate: String(Number(s.rate)),
  start_date: dayOf(s.start_date), end_date: dayOf(s.end_date), bill_from: dayOf(s.bill_from), status: s.status, notes: s.notes || '',
  contract_id: s.contract_id ? String(s.contract_id) : '', sla_plan_id: s.sla_plan_id ? String(s.sla_plan_id) : '',
  tax_ids: s.tax_ids || [],
});

// Body for PUT from a stored service, with some fields changed.
const bodyOf = (s, changes = {}) => {
  const f = { ...fromService(s), ...changes };
  return {
    ...f, product_id: Number(f.product_id), company_id: Number(f.company_id) || 0, quantity: Number(f.quantity), rate: Number(f.rate),
    contract_id: Number(f.contract_id) || 0, sla_plan_id: Number(f.sla_plan_id) || 0, confirm_duplicate: true,
  };
};

// ---- add / edit a service ----
export const ServiceForm = ({ service, client: presetClient, contract: presetContract, onClose, onSaved }) => {
  const [client, setClient] = useState(presetClient || service?.client || null);
  const [products, setProducts] = useState(null);
  const [plans, setPlans] = useState([]);
  const [contracts, setContracts] = useState([]);
  const [taxes, setTaxes] = useState(null);
  const companies = useCompanies();
  const [form, setForm] = useState(() => (service ? fromService(service) : {
    company_id: '', product_id: '', description: '', reference: '', charge_type: 'monthly', quantity: '1', rate: '',
    start_date: presetContract && dayOf(presetContract.start_date) > todayISO() ? dayOf(presetContract.start_date) : todayISO(),
    end_date: '', bill_from: '', status: 'active', notes: '',
    contract_id: presetContract ? String(presetContract.id) : '', sla_plan_id: presetContract?.sla_plan_id ? String(presetContract.sla_plan_id) : '',
    tax_ids: [],
  }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [duplicate, setDuplicate] = useState('');
  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.value }));
  const billed = !!service?.billed_through;
  // The billing company: the default for a new service until one is chosen.
  const coId = form.company_id || String(defaultCompany(companies)?.id || '');
  const ofCompany = (ids, co) => (!taxes || !Number(co) ? (ids || []) : (ids || []).filter(tid => taxes.find(t => t.id === tid)?.company_id === Number(co)));
  const pickCompany = (v) => setForm(f => {
    const p = (products || []).find(x => String(x.id) === f.product_id);
    // The product's taxes of that company (a company has its own taxes).
    return { ...f, company_id: v, tax_ids: ofCompany(p && !service ? p.tax_ids : f.tax_ids, v) };
  });

  useEffect(() => {
    api('/api/products?status=all').then(d => setProducts(d.products)).catch(err => setError(err.message));
    api('/api/sla-plans?status=all').then(d => setPlans(d.plans)).catch(() => {});
    api('/api/taxes?status=all').then(d => setTaxes(d.taxes)).catch(() => setTaxes([]));
  }, []);
  // The client's contracts (changing the client clears the choice).
  const clientId = client?.id;
  useEffect(() => {
    if (!clientId) { setContracts([]); return; }
    api(`/api/contracts${qs({ client_id: clientId, state: 'all', page_size: 200 })}`).then(d => setContracts(d.contracts)).catch(() => {});
  }, [clientId]);
  const changeClient = (c) => { setClient(c); setForm(f => ({ ...f, contract_id: '' })); };

  // Choosing a contract fills in its default service level when none is set.
  const pickContract = (e) => {
    const id = e.target.value;
    const k = contracts.find(x => String(x.id) === id);
    setForm(f => ({ ...f, contract_id: id, sla_plan_id: !f.sla_plan_id && k?.sla_plan_id ? String(k.sla_plan_id) : f.sla_plan_id }));
  };

  const pickProduct = (e) => {
    const id = e.target.value;
    const p = products.find(x => String(x.id) === id);
    setForm(f => {
      const prev = products.find(x => String(x.id) === f.product_id);
      const next = { ...f, product_id: id };
      if (p && !service) {
        next.charge_type = p.charge_type;
        next.rate = String(Number(p.default_rate));
      }
      if (p && (!f.description.trim() || f.description === prev?.name)) next.description = p.name;
      // A new product brings its taxes (still changeable below).
      if (p && id !== f.product_id) next.tax_ids = ofCompany(p.tax_ids, f.company_id || coId);
      return next;
    });
  };

  const save = async (confirmDuplicate = false) => {
    setError('');
    if (!service && !client) { setError('Choose a client'); return; }
    if (!form.product_id) { setError('Choose a product'); return; }
    const quantity = num(form.quantity);
    const rate = num(form.rate === '' ? '0' : form.rate);
    if (!Number.isFinite(quantity) || quantity <= 0) { setError('Quantity must be more than 0'); return; }
    if (!Number.isFinite(rate) || rate < 0) { setError('Check the rate'); return; }
    setBusy(true);
    try {
      const body = {
        ...form, client_id: client?.id, product_id: Number(form.product_id), company_id: Number(coId) || 0, quantity, rate,
        end_date: form.charge_type === 'one_time' ? '' : form.end_date, confirm_duplicate: confirmDuplicate,
        contract_id: Number(form.contract_id) || 0, sla_plan_id: Number(form.sla_plan_id) || 0,
        tax_ids: form.tax_ids || [],
      };
      const d = service
        ? await api(`/api/subscriptions/${service.id}`, { method: 'PUT', body })
        : await api('/api/subscriptions', { method: 'POST', body });
      onSaved(d.service);
    } catch (err) {
      if (err.data?.duplicate) setDuplicate(err.message);
      else setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  // Inactive products can't be chosen for a new service, but an existing
  // service keeps showing its own.
  const choices = (products || []).filter(p => p.active || String(p.id) === form.product_id);
  const oneTime = form.charge_type === 'one_time';
  // Terminated contracts and inactive levels only show when already chosen.
  const contractChoices = contracts.filter(k => k.state !== 'terminated' || String(k.id) === form.contract_id);
  const planChoices = plans.filter(p => p.active || String(p.id) === form.sla_plan_id);
  const chosenPlan = plans.find(p => String(p.id) === form.sla_plan_id);

  return (
    <Modal wide title={service ? `Edit ${service.subscription_number}` : 'Add service'} onClose={onClose}
      footer={<>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button busy={busy} disabled={!products} onClick={() => save(false)}>{service ? 'Save changes' : 'Add service'}</Button>
      </>}>
      {!products ? <Spinner /> : (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <Field label="Client" required className="sm:col-span-3">
            {service || presetClient
              ? <div className={`${inputCls} bg-zinc-50 dark:bg-zinc-900/60`}><b>{client?.name}</b> <span className="font-mono text-xs text-zinc-500">{client?.client_number}</span></div>
              : <ClientPicker value={client} onChange={changeClient} />}
          </Field>
          {companies.length > 1 && (
            <Field label="Billed by" required className="sm:col-span-3"
              hint={billed ? 'Already billed: the company can\'t change. End this service and add a new one under the other company.' : 'This company\'s invoices carry this service (its logo, details, invoice numbers and taxes).'}>
              <CompanySelect companies={companies} value={coId} onChange={pickCompany} disabled={billed} />
            </Field>
          )}
          <Field label="Product" required className="sm:col-span-2">
            <Select value={form.product_id} onChange={pickProduct}>
              <option value="">Choose…</option>
              {choices.map(p => <option key={p.id} value={p.id}>{p.name}{p.code ? ` (${p.code})` : ''}{p.active ? '' : ' — inactive'}</option>)}
            </Select>
            {choices.length === 0 && <span className="block mt-1 text-[11px] text-amber-600">No products yet: add them on the Products page first.</span>}
          </Field>
          <Field label="Charge type" required>
            <Select value={form.charge_type} onChange={set('charge_type')} disabled={billed}>
              {Object.entries(CHARGE_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </Select>
          </Field>
          <Field label="Description on invoice" className="sm:col-span-2" hint="Defaults to the product name.">
            <Input value={form.description} onChange={set('description')} maxLength={300} />
          </Field>
          <Field label="Circuit ID / reference" hint="Link ID, site or branch.">
            <Input value={form.reference} onChange={set('reference')} maxLength={100} />
          </Field>
          <Field label="Quantity">
            <Input inputMode="decimal" value={form.quantity} onChange={set('quantity')} maxLength={10} />
          </Field>
          <Field label={`Rate (PKR ${CHARGE_SHORT[form.charge_type]})`} hint="This client's rate, per unit, before tax.">
            <Input inputMode="decimal" value={form.rate} onChange={set('rate')} maxLength={20} placeholder="0" />
          </Field>
          <Field label="Amount">
            <div className={`${inputCls} bg-zinc-50 dark:bg-zinc-900/60 font-semibold`}>
              {Number.isFinite(num(form.quantity) * num(form.rate || '0')) ? formatMoney(num(form.quantity) * num(form.rate || '0')) : '—'}
            </div>
          </Field>
          <Field label={oneTime ? 'Charge date' : 'Start date'} required hint={billed ? `Billed up to ${fmtDay(service.billed_through)}: can't change.` : oneTime ? 'Goes on the first invoice on or after this date.' : 'First day of service.'}>
            <Input type="date" value={form.start_date} onChange={set('start_date')} disabled={billed} />
          </Field>
          {!oneTime && (
            <Field label="End date" hint="Last day billed. Empty = ongoing.">
              <Input type="date" value={form.end_date} onChange={set('end_date')} min={form.start_date || undefined} />
            </Field>
          )}
          {!oneTime && (
            <Field label="Don't bill before" hint="Optional. For a service moved from an older system: the first day to bill here.">
              <Input type="date" value={form.bill_from} onChange={set('bill_from')} min={form.start_date || undefined} />
            </Field>
          )}
          {!oneTime && (
            <Field label="Status" hint="Suspended services are not billed. Resuming bills again from that day.">
              <Select value={form.status} onChange={set('status')}>
                <option value="active">Active</option>
                <option value="suspended">Suspended</option>
              </Select>
            </Field>
          )}
          <Field label="Contract" className="sm:col-span-2" hint={client ? (contractChoices.length ? 'Optional. The agreement this service falls under.' : 'This client has no contracts yet (Contracts page).') : 'Choose the client first.'}>
            <Select value={form.contract_id} onChange={pickContract} disabled={!client}>
              <option value="">None</option>
              {contractChoices.map(k => <option key={k.id} value={k.id}>{k.contract_number} · {k.title}{k.state === 'terminated' ? ' — terminated' : ''}</option>)}
            </Select>
          </Field>
          <Field label="Service level (SLA)" hint={chosenPlan ? slaTargets(chosenPlan) : 'Optional.'}>
            <Select value={form.sla_plan_id} onChange={set('sla_plan_id')}>
              <option value="">None</option>
              {planChoices.map(p => <option key={p.id} value={p.id}>{p.name}{p.active ? '' : ' — inactive'}</option>)}
            </Select>
          </Field>
          <Field label="Taxes" className="sm:col-span-3" hint="Worked out on each invoice line for this service. Filled in from the product.">
            {!taxes ? <Spinner /> : <TaxPicker taxes={taxes} companyId={coId} value={form.tax_ids} onChange={(ids) => setForm(f => ({ ...f, tax_ids: ids }))} />}
          </Field>
          <BillingPreview form={{ ...form, rate: form.rate || '0' }} />
          <Field label="Notes" className="sm:col-span-3"><Textarea rows={2} value={form.notes} onChange={set('notes')} maxLength={5000} /></Field>
        </div>
      )}
      {duplicate && (
        <div className="mt-3 p-3 rounded-lg border border-amber-300 dark:border-amber-800 bg-amber-50 dark:bg-amber-950/30 text-sm text-amber-800 dark:text-amber-300">
          <p>{duplicate}. Add it anyway?</p>
          <div className="mt-2 flex gap-2">
            <Button variant="secondary" onClick={() => setDuplicate('')}>Go back</Button>
            <Button onClick={() => { setDuplicate(''); save(true); }}>Yes, save</Button>
          </div>
        </div>
      )}
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- end a service ----
const EndServiceForm = ({ service, onClose, onSaved }) => {
  const [end, setEnd] = useState(dayOf(service.end_date) || todayISO());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const save = async () => {
    setBusy(true);
    setError('');
    try {
      const d = await api(`/api/subscriptions/${service.id}`, { method: 'PUT', body: bodyOf(service, { end_date: end }) });
      onSaved(d.service);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  const amount = Number(service.amount);
  const last = service.charge_type === 'monthly' ? partMonths(amount, service.start_date, end).find(p => p.which !== 'first') : null;
  return (
    <Modal title={`End ${service.description}`} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} onClick={save}>End service</Button></>}>
      <Field label="Last day of service" required hint="Billed up to and including this day.">
        <Input type="date" value={end} min={dayOf(service.start_date)} onChange={(e) => setEnd(e.target.value)} />
      </Field>
      {last && <p className="mt-3 text-sm">Last month, {last.label}: {last.days} of {last.of} days → <b>{formatMoney(last.amount)}</b></p>}
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- the services table (all clients, or one) ----
export const ServicesTable = ({ services, showClient, empty }) => {
  const many = useCompanies().length > 1;
  return (
  <Table head={[
    'Service', ...(showClient ? ['Client'] : []), 'Charge', { label: 'Amount', right: true }, 'Period', 'State',
  ]}>
    {services.length === 0 && <tr><td colSpan={showClient ? 6 : 5} className="px-3 py-6 text-center text-zinc-500">{empty}</td></tr>}
    {services.map(s => (
      <tr key={s.id} onClick={() => navigate(`services/${s.id}`)}
        className="border-t border-zinc-100 dark:border-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer">
        <td className="px-3 py-2 min-w-[11rem]">
          <div className="font-semibold">{s.description}</div>
          <div className="text-[11px] text-zinc-500">
            <span className="font-mono">{s.subscription_number}</span>{s.reference && ` · ${s.reference}`}
            {s.sla_plan && <span className="text-indigo-600 dark:text-indigo-400"> · {s.sla_plan.name}</span>}
            {s.contract && <span> · {s.contract.contract_number}</span>}
            {many && s.company && <span> · {s.company.name}</span>}
          </div>
        </td>
        {showClient && (
          <td className="px-3 py-2 min-w-[10rem]"><div>{s.client?.name}</div><div className="font-mono text-[11px] text-zinc-500">{s.client?.client_number}</div></td>
        )}
        <td className="px-3 py-2 whitespace-nowrap">{CHARGE_TYPES[s.charge_type]}</td>
        <td className="px-3 py-2 text-right whitespace-nowrap">
          <div className="font-semibold">{formatMoney(s.amount)}</div>
          <div className="text-[11px] text-zinc-500">{qtyText(s.quantity)}{formatMoney(s.rate)} {CHARGE_SHORT[s.charge_type]}</div>
        </td>
        <td className="px-3 py-2 whitespace-nowrap text-xs">{period(s)}</td>
        <td className="px-3 py-2"><StateBadge state={s.state} /></td>
      </tr>
    ))}
  </Table>
  );
};

const Totals = ({ totals }) => (
  <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm mb-3">
    <span>Monthly recurring (MRC): <b>{formatMoney(totals.monthly)}</b></span>
    {totals.annual > 0 && <span>Annual: <b>{formatMoney(totals.annual)}</b></span>}
    <span className="text-zinc-500">{totals.count} service{totals.count === 1 ? '' : 's'} billing now · before tax</span>
  </div>
);

// ---- on the client's page ----
export const ClientServices = ({ client, onChanged }) => {
  const { can } = useAuth();
  const canManage = can('manage_billing');
  const [state, setState] = useState('current');
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    api(`/api/subscriptions${qs({ client_id: client.id, state, page_size: 200 })}`).then(setData).catch(err => setError(err.message));
  }, [client.id, state, reload]);

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
        <h3 className="text-sm font-semibold flex items-center gap-1.5"><Network className="w-4 h-4 text-indigo-500" /> Services</h3>
        <div className="flex gap-2">
          <Select value={state} onChange={(e) => setState(e.target.value)} className="!w-auto !py-1.5">
            <option value="current">Current</option>
            <option value="ended">Ended / billed</option>
            <option value="all">All</option>
          </Select>
          {canManage && client.status !== 'archived' && <Button icon={Plus} onClick={() => setAdding(true)}>Add service</Button>}
        </div>
      </div>
      <ErrorText>{error}</ErrorText>
      {!data ? (!error && <Spinner />) : (
        <>
          <Totals totals={data.totals} />
          <ServicesTable services={data.services}
            empty={state === 'current' ? (canManage ? 'No services yet. Use "Add service".' : 'No current services.') : 'None.'} />
        </>
      )}
      {adding && (
        <ServiceForm client={client} onClose={() => setAdding(false)}
          onSaved={() => { setAdding(false); setReload(r => r + 1); onChanged?.(); }} />
      )}
    </div>
  );
};

// ---- SLA credit check: a month's downtime → uptime % → credit owed ----
const prevMonth = () => {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
};

const SlaCreditCheck = ({ service: s }) => {
  const plan = s.sla_plan;
  const [month, setMonth] = useState(prevMonth);
  const [hours, setHours] = useState('');
  const [mins, setMins] = useState('');
  const downtime = Math.round((Number(hours) || 0) * 60 + (Number(mins) || 0));
  const days = /^\d{4}-\d{2}$/.test(month) ? daysInMonth(month) : 30;
  const monthly = s.charge_type === 'annual' ? Number(s.amount) / 12 : Number(s.amount);
  const r = slaCredit(plan, monthly, downtime, days);
  return (
    <Card className="p-4">
      <h3 className="text-sm font-semibold mb-1 flex items-center gap-1.5"><ShieldCheck className="w-4 h-4 text-indigo-500" /> {plan.name}</h3>
      <p className="text-xs text-zinc-500 mb-3">{slaTargets(plan)}{plan.support_note ? ` (${plan.support_note})` : ''}</p>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="text-xs text-zinc-700 dark:text-zinc-300">
          <CreditSteps plan={plan} />
          {plan.description && <p className="mt-2 text-zinc-500 whitespace-pre-wrap">{plan.description}</p>}
        </div>
        <div>
          <div className="text-xs font-semibold mb-2 flex items-center gap-1.5"><Calculator className="w-3.5 h-3.5" /> SLA credit check</div>
          <div className="flex flex-wrap items-end gap-2">
            <Field label="Month"><Input type="month" value={month} onChange={(e) => setMonth(e.target.value)} className="!w-40" /></Field>
            <Field label="Downtime">
              <div className="flex items-center gap-1.5">
                <Input inputMode="numeric" value={hours} onChange={(e) => setHours(e.target.value)} placeholder="0" maxLength={4} className="!w-16" /> <span className="text-xs text-zinc-500">h</span>
                <Input inputMode="numeric" value={mins} onChange={(e) => setMins(e.target.value)} placeholder="0" maxLength={2} className="!w-16" /> <span className="text-xs text-zinc-500">min</span>
              </div>
            </Field>
          </div>
          <div className="mt-3 text-sm space-y-0.5">
            <div>Uptime: <b className={r.met ? 'text-emerald-600' : 'text-rose-600'}>{pctText(r.uptime)}</b> {Number(plan.uptime_target) > 0 && <span className="text-zinc-500">(target {pctText(plan.uptime_target)}{r.met ? ', met' : ', missed'})</span>}</div>
            <div className="text-xs text-zinc-500">{minutesText(downtime)} down of {days} days ({days * 24} hours)</div>
            {r.percent > 0
              ? <div>Credit owed: <b>{pctText(r.percent)}</b> of {formatMoney(monthly)} = <b>{formatMoney(r.amount)}</b> <span className="text-xs text-zinc-500">before tax</span></div>
              : <div className="text-zinc-500">No credit owed.</div>}
          </div>
          {r.percent > 0 && <p className="mt-2 text-[11px] text-zinc-500">Give it on the next invoice: add a line "SLA credit {month} – {s.description}" with rate −{Number(r.amount).toFixed(2)}.</p>}
        </div>
      </div>
    </Card>
  );
};

// ---- one service ----
const ServiceDetail = ({ id }) => {
  const [data, setData] = useState(null);
  const [taxes, setTaxes] = useState([]);
  useEffect(() => { api('/api/taxes?status=all').then(d => setTaxes(d.taxes)).catch(() => {}); }, []);
  const [error, setError] = useState('');
  const [dialog, setDialog] = useState(null); // 'edit' | 'end'
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    setError('');
    api(`/api/subscriptions/${id}`).then(setData).catch(err => setError(err.message));
  }, [id]);
  useEffect(() => { load(); }, [load]);

  if (error && !data) return <ErrorText>{error}</ErrorText>;
  if (!data) return <Spinner />;
  const s = data.service;
  const finished = ['ended', 'billed'].includes(s.state);

  const setStatus = async (status) => {
    setBusy(true);
    setError('');
    try {
      await api(`/api/subscriptions/${s.id}`, { method: 'PUT', body: bodyOf(s, { status }) });
      load();
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
          <div className="font-mono text-xs text-zinc-500">{s.subscription_number}</div>
          <h2 className="text-lg font-bold flex items-center gap-2">{s.description} <StateBadge state={s.state} /></h2>
          <button type="button" onClick={() => navigate(`clients/${s.client_id}`)} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer">
            {s.client?.name} <span className="font-mono text-xs">{s.client?.client_number}</span>
          </button>
        </div>
        {data.can_manage && (
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" icon={Pencil} onClick={() => setDialog('edit')}>Edit</Button>
            {!finished && s.charge_type !== 'one_time' && (s.status === 'suspended'
              ? <Button variant="secondary" icon={PlayCircle} busy={busy} onClick={() => setStatus('active')}>Resume</Button>
              : <Button variant="secondary" icon={PauseCircle} busy={busy} onClick={() => { if (window.confirm('Suspend this service? It will not be billed until resumed.')) setStatus('suspended'); }}>Suspend</Button>)}
            {!finished && s.charge_type !== 'one_time' && <Button variant="secondary" icon={CalendarX} onClick={() => setDialog('end')}>End service</Button>}
          </div>
        )}
      </div>
      <ErrorText>{error}</ErrorText>
      <Card className="p-4 grid grid-cols-1 sm:grid-cols-3 gap-4">
        {row('Billed by', s.company?.name)}
        {row('Product', s.product?.name)}
        {row('Circuit ID / reference', s.reference)}
        {row('Charge type', CHARGE_TYPES[s.charge_type])}
        {row('Rate', `${qtyText(s.quantity)}${formatMoney(s.rate)} ${CHARGE_SHORT[s.charge_type]}${s.product?.unit ? ` (per ${s.product.unit})` : ''}`)}
        {row('Amount', `${formatMoney(s.amount)} ${CHARGE_SHORT[s.charge_type]} before tax`)}
        {row(s.charge_type === 'one_time' ? 'Charge date' : 'Period', period(s))}
        {row('Billed up to', s.billed_through ? fmtDay(s.billed_through) : 'Not billed yet')}
        {s.bill_from && row("Don't bill before", fmtDay(s.bill_from))}
        <div>
          <div className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">Contract</div>
          {s.contract
            ? <button type="button" onClick={() => navigate(`contracts/${s.contract.id}`)} className="text-sm text-left text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer"><span className="font-mono text-xs">{s.contract.contract_number}</span> {s.contract.title}</button>
            : <div className="text-sm">—</div>}
        </div>
        {row('Service level (SLA)', s.sla_plan ? `${s.sla_plan.name}: ${slaTargets(s.sla_plan)}` : 'None')}
        {row('Taxes', (s.tax_ids || []).map(tid => taxes.find(t => t.id === tid)).filter(Boolean)
          .map(t => `${t.kind === 'deduct' ? 'less ' : ''}${taxShort(t)}${t.compound ? ' (compound)' : ''}`).join(', ') || 'None')}
        {s.notes && <div className="sm:col-span-3">{row('Notes', s.notes)}</div>}
      </Card>
      {s.sla_plan && s.charge_type !== 'one_time' && <SlaCreditCheck service={s} />}
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
      {dialog === 'edit' && <ServiceForm service={s} onClose={() => setDialog(null)} onSaved={() => { setDialog(null); load(); }} />}
      {dialog === 'end' && <EndServiceForm service={s} onClose={() => setDialog(null)} onSaved={() => { setDialog(null); load(); }} />}
    </div>
  );
};

// ---- all services ----
export const ServicesPage = ({ arg, query }) => {
  const { can } = useAuth();
  const canManage = can('manage_billing');
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  const [state, setState] = useState('current');
  const [type, setType] = useState('');
  const [product, setProduct] = useState(query?.get('product') || '');
  const [sla, setSla] = useState(query?.get('sla') || '');
  const [plans, setPlans] = useState([]);
  const [products, setProducts] = useState([]);
  const [companyId, setCompanyId] = useState('');
  const companies = useCompanies();
  const [page, setPage] = useState(1);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);
  const [reload, setReload] = useState(0);

  useEffect(() => { setProduct(query?.get('product') || ''); setSla(query?.get('sla') || ''); }, [query]);
  useEffect(() => {
    api('/api/products?status=all').then(d => setProducts(d.products)).catch(() => {});
    api('/api/sla-plans?status=all').then(d => setPlans(d.plans)).catch(() => {});
  }, []);
  useEffect(() => { const t = setTimeout(() => setDebounced(q.trim()), 300); return () => clearTimeout(t); }, [q]);
  useEffect(() => { setPage(1); }, [debounced, state, type, product, sla, companyId]);
  useEffect(() => {
    const ctrl = new AbortController();
    setError('');
    api(`/api/subscriptions${qs({ q: debounced, state, charge_type: type, product_id: product, sla_plan_id: sla, company_id: companyId, page })}`, { signal: ctrl.signal })
      .then(setData).catch(err => { if (err.name !== 'AbortError') setError(err.message); });
    return () => ctrl.abort();
  }, [debounced, state, type, product, sla, companyId, page, reload]);

  const openId = /^\d+$/.test(arg || '') ? arg : null;
  if (openId) {
    return (
      <>
        <button type="button" onClick={() => window.history.length > 1 ? window.history.back() : navigate('services')} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline mb-4 cursor-pointer">← Back</button>
        <ServiceDetail key={openId} id={openId} />
      </>
    );
  }

  return (
    <>
      <PageHeader icon={Network} title="Services" subtitle="What each client takes, at their rate."
        actions={canManage && <Button icon={Plus} onClick={() => setAdding(true)}>Add service</Button>} />
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="relative flex-1 min-w-[14rem] max-w-md">
          <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Client, service, circuit ID, SUB no.…" className={`${inputCls} pl-8`} />
        </span>
        {companies.length > 1 && <CompanySelect companies={companies} value={companyId} allLabel="All companies" onChange={setCompanyId} className="!w-auto" />}
        <Select value={product} onChange={(e) => setProduct(e.target.value)} className="!w-auto">
          <option value="">All products</option>
          {products.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </Select>
        <Select value={sla} onChange={(e) => setSla(e.target.value)} className="!w-auto">
          <option value="">Any SLA</option>
          <option value="none">No SLA</option>
          {plans.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </Select>
        <Select value={type} onChange={(e) => setType(e.target.value)} className="!w-auto">
          <option value="">All charge types</option>
          {Object.entries(CHARGE_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </Select>
        <Select value={state} onChange={(e) => setState(e.target.value)} className="!w-auto">
          <option value="current">Current</option>
          <option value="suspended">Suspended</option>
          <option value="ended">Ended / billed</option>
          <option value="all">All</option>
        </Select>
        {data && <span className="text-sm text-zinc-500">{data.total} service{data.total === 1 ? '' : 's'}</span>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data && !error ? <Spinner /> : data && (
        <>
          <Totals totals={data.totals} />
          <ServicesTable showClient services={data.services}
            empty={debounced || type || product || sla || state !== 'current' ? 'No services match.' : canManage ? 'No services yet. Add them here or from a client\'s page.' : 'No services yet.'} />
          <Pager page={data.page} pageSize={data.page_size} total={data.total} onPage={setPage} />
        </>
      )}
      {adding && (
        <ServiceForm onClose={() => setAdding(false)}
          onSaved={(s) => { setAdding(false); setReload(r => r + 1); navigate(`services/${s.id}`); }} />
      )}
    </>
  );
};
