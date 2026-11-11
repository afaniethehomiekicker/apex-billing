import React, { useCallback, useEffect, useState } from 'react';
import {
  FileText, Plus, Search, CalendarPlus, Send, Printer, Trash2, Ban, Save, ListPlus, History, X, Wallet,
} from 'lucide-react';
import { api, qs } from '../lib/api';
import { useAuth, navigate } from '../lib/auth';
import {
  formatMoney, fmtDay, fmtDateTime, fmtMonth, dayOf, todayISO, monthISO, linePeriod, lineAmount, INVOICE_STATUS, payState, PAYMENT_STATUS,
  invoiceTaxes, taxShort, pctText,
} from '../lib/format';
import {
  PageHeader, Button, Field, Input, Select, Textarea, Badge, Modal, Pager, Table, Spinner, ErrorText, Card, inputCls,
} from '../components/ui';
import { ClientPicker } from './ServicesPage';
import { TaxPicker } from '../components/TaxPicker';
import { CompanySelect } from '../components/CompanySelect';
import { useCompanies, defaultCompany, companyName } from '../lib/companies';

// Invoices: the monthly run (drafts → review → issue all), manual
// invoices, editing drafts, issuing, cancelling, printing.
// #/invoices lists them; #/invoices/12 opens one; #/print/12 prints it.

export const InvoiceStatus = ({ status }) => {
  const [label, tone] = INVOICE_STATUS[status] || [status, 'zinc'];
  return <Badge tone={tone}>{label}</Badge>;
};

const nextMonthISO = () => {
  const d = new Date();
  // From the 20th on, the run you're most likely preparing is next month's.
  if (d.getDate() >= 20) d.setMonth(d.getMonth() + 1, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
};

// ---- the monthly run ----
const GenerateForm = ({ onClose, onDone }) => {
  const companies = useCompanies();
  const many = companies.length > 1;
  const [companyId, setCompanyId] = useState('');
  const [month, setMonth] = useState(nextMonthISO);
  const [invDate, setInvDate] = useState(`${nextMonthISO()}-01`);
  const [preview, setPreview] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [open, setOpen] = useState({});

  const run = async (save) => {
    setBusy(true);
    setError('');
    try {
      const d = await api('/api/invoices/generate', { method: 'POST', body: { month, invoice_date: invDate, company_id: Number(companyId) || 0, preview: !save } });
      if (save) onDone(d);
      else setPreview(d);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal wide title="Make monthly invoices" onClose={onClose}
      footer={<>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        {!preview
          ? <Button busy={busy} onClick={() => run(false)}>Show what will be billed</Button>
          : <Button busy={busy} disabled={preview.clients.length === 0} onClick={() => run(true)}>
            Make {preview.clients.length} draft{preview.clients.length === 1 ? '' : 's'}
          </Button>}
      </>}>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <Field label="Month to bill" hint="Monthly services are billed in advance for this month.">
          <Input type="month" value={month} onChange={(e) => { setMonth(e.target.value); setInvDate(`${e.target.value}-01`); setPreview(null); }} />
        </Field>
        <Field label="Invoice date" hint="Due date = this + each client's payment terms.">
          <Input type="date" value={invDate} onChange={(e) => { setInvDate(e.target.value); setPreview(null); }} />
        </Field>
        {many && (
          <Field label="Billing company" hint="Each company's services go on that company's own invoice." className="sm:col-span-2">
            <CompanySelect companies={companies} value={companyId} allLabel="All companies" onChange={(v) => { setCompanyId(v); setPreview(null); }} />
          </Field>
        )}
      </div>
      <p className="mt-3 text-xs text-zinc-500">
        Each active client gets one draft{many ? ' per billing company' : ''} with: this month for monthly services, any part month or month not yet billed before it,
        one-time charges not yet billed, and yearly charges whose year starts by the end of the month. Suspended services are skipped.
        Nothing is final until you issue the drafts. Running this again later only adds what's new.
      </p>
      {preview && (
        <div className="mt-4">
          {preview.clients.length === 0 ? (
            <p className="text-sm p-3 rounded-lg bg-zinc-100 dark:bg-zinc-900">Nothing to bill for {fmtMonth(`${month}-01`)}: everything due is already on an invoice.</p>
          ) : (
            <>
              <div className="text-sm mb-2">
                <b>{preview.clients.length}</b> {many ? 'invoice' : 'client'}{preview.clients.length === 1 ? '' : 's'}, total <b>{formatMoney(preview.total)}</b> with taxes.
              </div>
              <div className="max-h-[50vh] overflow-y-auto rounded-lg border border-zinc-200 dark:border-zinc-800 divide-y divide-zinc-100 dark:divide-zinc-800">
                {preview.clients.map(c => (
                  <div key={`${c.client_id}-${c.company_id}`} className="p-2.5 text-sm">
                    <button type="button" className="w-full flex flex-wrap items-center justify-between gap-2 text-left cursor-pointer"
                      onClick={() => setOpen(o => ({ ...o, [`${c.client_id}-${c.company_id}`]: !o[`${c.client_id}-${c.company_id}`] }))}>
                      <span><b>{c.name}</b> <span className="font-mono text-[11px] text-zinc-500">{c.client_number}</span>
                        {many && <span className="ml-2"><Badge tone="indigo">{c.company_name}</Badge></span>}
                        {c.earlier > 0 && <span className="ml-2"><Badge tone="amber">{c.earlier} earlier period{c.earlier === 1 ? '' : 's'}</Badge></span>}
                        {c.adds_to_draft && <span className="ml-2"><Badge>adds to existing draft</Badge></span>}
                      </span>
                      <span className="font-semibold">{formatMoney(c.total)}</span>
                    </button>
                    {open[`${c.client_id}-${c.company_id}`] && (
                      <table className="w-full mt-2 text-xs">
                        <tbody>
                          {c.lines.map((l, i) => (
                            <tr key={i} className="border-t border-zinc-100 dark:border-zinc-800">
                              <td className="py-1 pr-2">{l.description}{l.reference && <span className="text-zinc-500"> · {l.reference}</span>}</td>
                              <td className="py-1 pr-2 text-zinc-500">{linePeriod(l)}</td>
                              <td className="py-1 text-right whitespace-nowrap">{formatMoney(l.amount)}</td>
                            </tr>
                          ))}
                          {(c.tax_lines || []).map((t, k) => (
                            <tr key={`t${k}`} className="border-t border-zinc-100 dark:border-zinc-800 text-zinc-500">
                              <td className="py-1" colSpan={2}>{t.kind === 'deduct' ? 'Less: ' : ''}{t.name} {pctText(t.rate)}</td>
                              <td className="py-1 text-right">{t.kind === 'deduct' ? '−' : ''}{formatMoney(t.amount)}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </div>
                ))}
              </div>
              <p className="mt-2 text-[11px] text-zinc-500">"Earlier periods" are part months or months before {fmtMonth(`${month}-01`)} that were never billed. Click a client to see the lines.</p>
            </>
          )}
        </div>
      )}
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- a new manual invoice ----
const NewInvoiceForm = ({ client: preset, onClose, onCreated }) => {
  const companies = useCompanies();
  const [companyId, setCompanyId] = useState('');
  const coId = companyId || defaultCompany(companies)?.id || '';
  const [client, setClient] = useState(preset || null);
  const [date, setDate] = useState(todayISO());
  const [withDue, setWithDue] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const create = async () => {
    if (!client) { setError('Choose a client'); return; }
    setBusy(true);
    setError('');
    try {
      const d = await api('/api/invoices', { method: 'POST', body: { client_id: client.id, company_id: Number(coId) || 0, invoice_date: date, lines: [] } });
      if (withDue) await api(`/api/invoices/${d.invoice.id}/add-due`, { method: 'POST', body: {} });
      onCreated(d.invoice);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal title="New invoice" onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} onClick={create}>Create draft</Button></>}>
      <div className="space-y-3">
        <Field label="Client" required>
          {preset ? <div className={`${inputCls} bg-zinc-50 dark:bg-zinc-900/60`}><b>{client.name}</b></div> : <ClientPicker value={client} onChange={setClient} />}
        </Field>
        {companies.length > 1 && (
          <Field label="Billing company" required hint="The invoice is printed with this company's logo, details and number.">
            <CompanySelect companies={companies} value={coId} onChange={setCompanyId} />
          </Field>
        )}
        <Field label="Invoice date"><Input type="date" value={date} onChange={(e) => setDate(e.target.value)} /></Field>
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" className="mt-1" checked={withDue} onChange={(e) => setWithDue(e.target.checked)} />
          <span>Add the client's service charges due up to the end of this month (not yet billed){companies.length > 1 ? ' that this company bills' : ''}. Otherwise start empty and type the lines.</span>
        </label>
        <ErrorText>{error}</ErrorText>
      </div>
    </Modal>
  );
};

// ---- the invoice list (all, or one client's) ----
export const InvoicesTable = ({ invoices, showClient = true, empty }) => {
  const companies = useCompanies();
  const many = companies.length > 1;
  return (
  <Table head={['Invoice', ...(showClient ? ['Client'] : []), 'Date', 'Due', { label: 'Total', right: true }, 'Status']}>
    {invoices.length === 0 && <tr><td colSpan={showClient ? 6 : 5} className="px-3 py-6 text-center text-zinc-500">{empty}</td></tr>}
    {invoices.map(v => (
      <tr key={v.id} onClick={() => navigate(`invoices/${v.id}`)}
        className="border-t border-zinc-100 dark:border-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer">
        <td className="px-3 py-2 whitespace-nowrap">
          <div className="font-mono font-semibold">{v.invoice_number || <span className="text-zinc-400 font-sans font-normal italic">draft #{v.id}</span>}</div>
          <div className="text-[11px] text-zinc-500">{v.billing_month ? fmtMonth(v.billing_month) : 'Manual'}{many && ` · ${v.issuer?.name || v.company?.name || companyName(companies, v.company_id)}`}</div>
        </td>
        {showClient && (
          <td className="px-3 py-2 min-w-[10rem]"><div>{v.client?.name || v.bill_name}</div><div className="font-mono text-[11px] text-zinc-500">{v.client?.client_number}</div></td>
        )}
        <td className="px-3 py-2 whitespace-nowrap">{fmtDay(v.invoice_date)}</td>
        <td className="px-3 py-2 whitespace-nowrap">{fmtDay(v.due_date)}</td>
        <td className="px-3 py-2 text-right whitespace-nowrap font-semibold">{formatMoney(v.total, v.currency)}</td>
        <td className="px-3 py-2"><PayBadge inv={v} /></td>
      </tr>
    ))}
  </Table>
  );
};

// Issued invoices show their payment state; drafts / cancelled their status.
export const PayBadge = ({ inv }) => {
  const ps = payState(inv);
  if (!ps) return <InvoiceStatus status={inv.status} />;
  return (
    <span className="inline-flex flex-col items-start gap-0.5">
      <Badge tone={ps[1]}>{ps[0]}</Badge>
      {ps[2] > 0 && Number(inv.amount_paid) > 0 && <span className="text-[11px] text-zinc-500 whitespace-nowrap">{formatMoney(ps[2], inv.currency)} left</span>}
    </span>
  );
};

// On a client's page.
export const ClientInvoices = ({ client }) => {
  const { can } = useAuth();
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);
  useEffect(() => {
    api(`/api/invoices${qs({ client_id: client.id, status: 'all', page_size: 100 })}`).then(setData).catch(err => setError(err.message));
  }, [client.id]);
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
        <h3 className="text-sm font-semibold flex items-center gap-1.5"><FileText className="w-4 h-4 text-indigo-500" /> Invoices</h3>
        {can('manage_billing') && client.status !== 'archived' && <Button variant="secondary" icon={Plus} onClick={() => setAdding(true)}>New invoice</Button>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data ? (!error && <Spinner />) : <InvoicesTable showClient={false} invoices={data.invoices} empty="No invoices yet." />}
      {adding && <NewInvoiceForm client={client} onClose={() => setAdding(false)} onCreated={(inv) => navigate(`invoices/${inv.id}`)} />}
    </div>
  );
};

// ---- one invoice ----
const toForm = (inv) => ({
  company_id: inv.company_id,
  invoice_date: dayOf(inv.invoice_date), due_date: dayOf(inv.due_date), tax_percent: String(Number(inv.tax_percent)),
  notes: inv.notes || '',
  lines: (inv.lines || []).map(l => ({
    ...l, quantity: String(Number(l.quantity)), rate: String(Number(l.rate)),
    tax_ids: (l.taxes || []).map(t => t.tax_id).filter(Boolean),
  })),
});

// An invoice's taxes as stored; an older invoice with one tax % shows it as one line.
const storedTaxLines = (inv) => {
  if (inv.tax_lines?.length) return inv.tax_lines;
  if (Number(inv.tax_amount)) return [{ name: 'Sales tax', rate: Number(inv.tax_percent), kind: 'add', base: inv.subtotal, amount: Number(inv.tax_amount) }];
  return [];
};
const n = (v) => Number(String(v ?? '').replace(/,/g, ''));

const CancelForm = ({ invoice, onClose, onDone }) => {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const save = async () => {
    setBusy(true);
    setError('');
    try {
      await api(`/api/invoices/${invoice.id}/cancel`, { method: 'POST', body: { reason } });
      onDone();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal title={`Cancel ${invoice.invoice_number}`} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Keep it</Button><Button variant="danger" busy={busy} onClick={save}>Cancel invoice</Button></>}>
      <p className="text-sm mb-3">The invoice stays on record as cancelled and its number is not reused. The service charges on it become unbilled,
        so the next monthly run (or a new invoice) bills them again.</p>
      <Field label="Reason" required><Textarea autoFocus value={reason} onChange={(e) => setReason(e.target.value)} maxLength={1000} /></Field>
      <div className="mt-2"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

const InvoiceDetail = ({ id }) => {
  const { can } = useAuth();
  const [data, setData] = useState(null);
  const [form, setForm] = useState(null);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [cancelling, setCancelling] = useState(false);
  const [taxes, setTaxes] = useState([]);
  const companies = useCompanies();
  useEffect(() => { api('/api/taxes?status=all').then(d => setTaxes(d.taxes)).catch(() => {}); }, []);

  const load = useCallback(() => {
    setError('');
    api(`/api/invoices/${id}`).then(d => { setData(d); setForm(toForm(d.invoice)); setDirty(false); }).catch(err => setError(err.message));
  }, [id]);
  useEffect(() => { load(); }, [load]);

  if (error && !data) return <ErrorText>{error}</ErrorText>;
  if (!data || !form) return <Spinner />;
  const inv = data.invoice;
  const draft = inv.status === 'draft';
  const editable = draft && data.can_manage;
  const cur = inv.currency || 'PKR';
  // Heading / unit of a service line's product (used when the line has none).
  const prodInfoMap = new Map((data.line_info || []).map(x => [x.line_id, x]));
  const prodInfo = (l) => (l.id ? prodInfoMap.get(l.id) : null);
  const headingChoices = [...new Set([...form.lines.map(l => (l.category || '').trim()), ...(data.line_info || []).map(x => (x.category || '').trim())].filter(Boolean))];

  const change = (patch) => { setForm(f => ({ ...f, ...patch })); setDirty(true); };
  const setLine = (i, k, v) => change({ lines: form.lines.map((l, j) => (j === i ? { ...l, [k]: v } : l)) });
  const amountOf = (l) => lineAmount(n(l.quantity), n(l.rate), l.prorate_days, l.prorate_of);
  // A line's taxes: the chosen taxes at today's rates (what saving will
  // use); one no longer in the list keeps the rate it was copied with.
  const lineTaxObjs = (l) => (l.tax_ids || []).map(tid => {
    const t = taxes.find(x => x.id === tid);
    if (t) return { ...t, tax_id: t.id };
    return (l.taxes || []).find(x => x.tax_id === tid);
  }).filter(Boolean);
  const calc = dirty
    ? invoiceTaxes(form.lines.map(l => ({ amount: Number.isFinite(amountOf(l)) ? amountOf(l) : 0, taxes: lineTaxObjs(l) })), n(form.tax_percent) || 0)
    : { subtotal: Number(inv.subtotal), taxLines: storedTaxLines(inv), total: Number(inv.total) };
  const { subtotal, total } = calc;

  const act = async (what, fn) => {
    setBusy(what);
    setError('');
    try {
      await fn();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy('');
    }
  };
  const hasServiceLines = form.lines.some(l => l.subscription_id);
  // A typed-in draft can move to another company; its lines' taxes must
  // then be that company's, so they are cleared.
  const changeCompany = (v) => {
    const id = Number(v);
    change({ company_id: id, lines: form.lines.map(l => ({ ...l, tax_ids: (l.tax_ids || []).filter(tid => taxes.find(t => t.id === tid)?.company_id === id) })) });
  };
  const saveBody = () => ({
    company_id: Number(form.company_id) || 0,
    invoice_date: form.invoice_date, due_date: form.due_date, notes: form.notes,
    ...(Number(inv.tax_percent) > 0 ? { tax_percent: n(form.tax_percent) || 0 } : {}),
    lines: form.lines.map(l => ({
      id: l.id || 0, description: l.description, reference: l.reference || '', category: l.category || '', unit: l.unit || '', quantity: n(l.quantity), rate: n(l.rate), tax_ids: l.tax_ids || [],
    })),
  });
  const save = () => act('save', async () => { await api(`/api/invoices/${inv.id}`, { method: 'PUT', body: saveBody() }); load(); });
  const addDue = () => act('due', async () => {
    if (dirty) await api(`/api/invoices/${inv.id}`, { method: 'PUT', body: saveBody() });
    const d = await api(`/api/invoices/${inv.id}/add-due`, { method: 'POST', body: {} });
    load();
    if (!d.added) setError('No service charges are due for this client up to the end of the invoice month.');
  });
  const issue = () => act('issue', async () => {
    if (!window.confirm(`Issue this invoice to ${inv.bill_name} for ${formatMoney(total, cur)}? It gets its number and can't be changed after this.`)) return;
    if (dirty) await api(`/api/invoices/${inv.id}`, { method: 'PUT', body: saveBody() });
    const d = await api('/api/invoices/issue', { method: 'POST', body: { ids: [inv.id] } });
    if (d.skipped.length) throw new Error(`Not issued: ${d.skipped[0].reason}`);
    load();
  });
  const remove = () => act('delete', async () => {
    if (!window.confirm('Delete this draft? Its service charges become unbilled again.')) return;
    await api(`/api/invoices/${inv.id}`, { method: 'DELETE' });
    navigate('invoices');
  });

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="text-xs text-zinc-500">{inv.billing_month ? `Monthly invoice · ${fmtMonth(inv.billing_month)}` : 'Manual invoice'}</div>
          <h2 className="text-lg font-bold flex items-center gap-2">
            <span className="font-mono">{inv.invoice_number || `Draft #${inv.id}`}</span> <PayBadge inv={inv} />
          </h2>
          <button type="button" onClick={() => navigate(`clients/${inv.client_id}`)} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer">
            {inv.bill_name} <span className="font-mono text-xs">{inv.client?.client_number}</span>
          </button>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" icon={Printer} onClick={() => { window.location.hash = `#/print/${inv.id}`; }}>{draft ? 'Preview' : 'Print / PDF'}</Button>
          {editable && <>
            <Button variant="secondary" icon={Trash2} busy={busy === 'delete'} onClick={remove}>Delete draft</Button>
            <Button variant="secondary" icon={Save} busy={busy === 'save'} disabled={!dirty} onClick={save}>Save</Button>
            <Button icon={Send} busy={busy === 'issue'} onClick={issue}>Issue</Button>
          </>}
          {inv.status === 'issued' && can('record_payments') && Number(inv.total) > Number(inv.amount_paid) && (
            <Button icon={Wallet} onClick={() => navigate(`payments?record=1&client=${inv.client_id}&invoice=${inv.id}`)}>Record payment</Button>
          )}
          {inv.status === 'issued' && data.can_manage && <Button variant="secondary" icon={Ban} onClick={() => setCancelling(true)}>Cancel invoice</Button>}
        </div>
      </div>

      {inv.status === 'cancelled' && (
        <Card className="p-3 text-sm border-rose-300 dark:border-rose-900 bg-rose-50/60 dark:bg-rose-950/20">
          Cancelled {fmtDateTime(inv.cancelled_at)}: {inv.cancel_reason}
        </Card>
      )}

      <Card className="p-4 grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
        <Field label="Billed by" hint={editable && hasServiceLines && companies.length > 1 ? 'Set by its services.' : undefined}>
          {editable && !hasServiceLines && companies.length > 1
            ? <CompanySelect companies={companies} value={form.company_id} onChange={changeCompany} />
            : <div className="text-sm">{data.company?.company_name || companyName(companies, inv.company_id)}</div>}
        </Field>
        <Field label="Invoice date">
          {editable ? <Input type="date" value={form.invoice_date} onChange={(e) => change({ invoice_date: e.target.value })} /> : <div className="text-sm">{fmtDay(inv.invoice_date)}</div>}
        </Field>
        <Field label="Due date">
          {editable ? <Input type="date" value={form.due_date} min={form.invoice_date} onChange={(e) => change({ due_date: e.target.value })} /> : <div className="text-sm">{fmtDay(inv.due_date)}</div>}
        </Field>
        {Number(inv.tax_percent) > 0 ? (
          <Field label="Old single tax %" hint={editable ? 'From before taxes per service. Set 0 and choose taxes on the lines instead.' : undefined}>
            {editable ? <Input inputMode="decimal" value={form.tax_percent} onChange={(e) => change({ tax_percent: e.target.value })} maxLength={6} /> : <div className="text-sm">{Number(inv.tax_percent)}%</div>}
          </Field>
        ) : (
          <Field label="Taxes">
            <div className="text-sm">{calc.taxLines.length ? `${calc.taxLines.length} on this invoice` : 'None'}</div>
          </Field>
        )}
        <Field label="Bill to">
          <div className="text-sm">{inv.bill_name}<div className="text-xs text-zinc-500">{[inv.bill_city, inv.bill_ntn && `NTN ${inv.bill_ntn}`].filter(Boolean).join(' · ')}</div></div>
        </Field>
      </Card>

      <div>
        <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
          <h3 className="text-sm font-semibold">Lines</h3>
          {editable && (
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" icon={ListPlus} busy={busy === 'due'} onClick={addDue}>Add due service charges</Button>
              <Button variant="secondary" icon={Plus} onClick={() => change({ lines: [...form.lines, { description: '', reference: '', category: form.lines.length ? (form.lines[form.lines.length - 1].category || '') : '', unit: '', quantity: '1', rate: '', tax_ids: [] }] })}>Add line</Button>
            </div>
          )}
        </div>
        <div className="overflow-x-auto rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900">
          <datalist id="invoice-headings">{headingChoices.map(h => <option key={h} value={h} />)}</datalist>
          <table className="w-full text-sm">
            <thead className="bg-zinc-50 dark:bg-zinc-800/60 text-zinc-600 dark:text-zinc-300 text-xs">
              <tr>
                <th className="px-3 py-2 text-left">Description</th>
                <th className="px-3 py-2 text-left">Period</th>
                <th className="px-3 py-2 text-right w-24">Qty</th>
                <th className="px-3 py-2 text-right w-36">Rate</th>
                <th className="px-3 py-2 text-right w-36">Amount</th>
                {editable && <th className="w-10" />}
              </tr>
            </thead>
            <tbody>
              {form.lines.length === 0 && (
                <tr><td colSpan={6} className="px-3 py-6 text-center text-zinc-500">No lines yet. {editable && 'Add the due service charges, or add a line.'}</td></tr>
              )}
              {form.lines.map((l, i) => (
                <tr key={l.id || `new-${i}`} className="border-t border-zinc-100 dark:border-zinc-800 align-top">
                  <td className="px-3 py-2 min-w-[14rem]">
                    {editable ? (
                      <div className="space-y-1">
                        <Input value={l.description} onChange={(e) => setLine(i, 'description', e.target.value)} maxLength={300} placeholder="Description" />
                        <Input value={l.reference || ''} onChange={(e) => setLine(i, 'reference', e.target.value)} maxLength={100} placeholder="Circuit ID / reference (optional)" className="!py-1 text-xs" />
                        <div className="flex gap-1">
                          <Input value={l.category || ''} onChange={(e) => setLine(i, 'category', e.target.value)} maxLength={60} list="invoice-headings"
                            placeholder={prodInfo(l)?.category ? `Heading: ${prodInfo(l).category} (from product)` : 'Heading, e.g. IT Services (optional)'} className="!py-1 text-xs flex-1 min-w-0" />
                          <Input value={l.unit || ''} onChange={(e) => setLine(i, 'unit', e.target.value)} maxLength={40}
                            placeholder={prodInfo(l)?.unit ? `Unit: ${prodInfo(l).unit}` : 'Unit, e.g. Month'} className="!py-1 text-xs !w-32 shrink-0" />
                        </div>
                        <TaxPicker compact taxes={taxes} companyId={form.company_id} value={l.tax_ids} onChange={(ids) => setLine(i, 'tax_ids', ids)} />
                      </div>
                    ) : (
                      <>
                        <div>{l.description}</div>{l.reference && <div className="text-xs text-zinc-500">{l.reference}</div>}
                        {(l.category || prodInfo(l)?.category) && <div className="text-[11px] text-indigo-600 dark:text-indigo-400">{l.category || prodInfo(l).category}</div>}
                        {(l.taxes || []).length > 0 && <div className="text-[11px] text-zinc-500">{l.taxes.map(t => `${t.kind === 'deduct' ? '−' : ''}${taxShort(t)}`).join(' · ')}</div>}
                      </>
                    )}
                  </td>
                  <td className="px-3 py-2 text-xs text-zinc-600 dark:text-zinc-400 min-w-[9rem]">
                    {linePeriod(l) || <span className="text-zinc-400">{l.subscription_id ? '' : 'Typed-in charge'}</span>}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {editable ? <Input inputMode="decimal" value={l.quantity} onChange={(e) => setLine(i, 'quantity', e.target.value)} className="text-right" /> : Number(l.quantity)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {editable ? <Input inputMode="decimal" value={l.rate} onChange={(e) => setLine(i, 'rate', e.target.value)} className="text-right" placeholder={l.subscription_id ? '' : '0 (negative = discount)'} /> : formatMoney(l.rate, cur)}
                  </td>
                  <td className="px-3 py-2 text-right whitespace-nowrap font-semibold">{Number.isFinite(amountOf(l)) ? formatMoney(amountOf(l), cur) : '—'}</td>
                  {editable && (
                    <td className="px-1 py-2">
                      <button type="button" aria-label="Remove line" title={l.subscription_id ? 'Remove (the charge becomes unbilled again)' : 'Remove'}
                        className="p-1.5 rounded text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800 cursor-pointer"
                        onClick={() => change({ lines: form.lines.filter((_, j) => j !== i) })}><X className="w-4 h-4" /></button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
            <tfoot className="text-sm">
              <tr className="border-t border-zinc-200 dark:border-zinc-800"><td colSpan={4} className="px-3 py-1.5 text-right text-zinc-500">Subtotal</td><td className="px-3 py-1.5 text-right">{formatMoney(subtotal, cur)}</td>{editable && <td />}</tr>
              {calc.taxLines.map((t, k) => (
                <tr key={k}>
                  <td colSpan={4} className="px-3 py-1.5 text-right text-zinc-500">
                    {t.kind === 'deduct' ? 'Less: ' : ''}{t.name} {pctText(t.rate)}
                    <span className="text-[11px]"> on {formatMoney(t.base, cur)}</span>
                  </td>
                  <td className="px-3 py-1.5 text-right">{t.kind === 'deduct' ? '−' : ''}{formatMoney(t.amount, cur)}</td>{editable && <td />}
                </tr>
              ))}
              <tr><td colSpan={4} className="px-3 py-2 text-right font-bold">Total</td><td className="px-3 py-2 text-right font-bold text-base">{formatMoney(total, cur)}</td>{editable && <td />}</tr>
            </tfoot>
          </table>
        </div>
      </div>

      {inv.status !== 'draft' && (
        <div>
          <h3 className="text-sm font-semibold mb-2 flex items-center gap-1.5"><Wallet className="w-4 h-4 text-indigo-500" /> Payments</h3>
          {data.payments.length === 0 ? <p className="text-sm text-zinc-500">No payments yet.</p> : (
            <Table head={['Payment', 'Date', 'Status', { label: 'Applied', right: true }]}>
              {data.payments.map(p => (
                <tr key={p.payment_id} onClick={() => navigate(`payments/${p.payment_id}`)} className="border-t border-zinc-100 dark:border-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer">
                  <td className="px-3 py-2 font-mono">{p.payment_number}</td>
                  <td className="px-3 py-2">{fmtDay(p.payment_date)}</td>
                  <td className="px-3 py-2"><Badge tone={(PAYMENT_STATUS[p.status] || [])[1]}>{(PAYMENT_STATUS[p.status] || [p.status])[0]}</Badge></td>
                  <td className="px-3 py-2 text-right">{formatMoney(p.amount, cur)}</td>
                </tr>
              ))}
            </Table>
          )}
          {inv.status === 'issued' && (
            <p className="mt-2 text-sm">Paid (verified) <b>{formatMoney(inv.amount_paid, cur)}</b> · Balance <b>{formatMoney(Math.max(0, Number(inv.total) - Number(inv.amount_paid)), cur)}</b></p>
          )}
        </div>
      )}

      <Field label="Notes (printed on the invoice)">
        {editable ? <Textarea rows={2} value={form.notes} onChange={(e) => change({ notes: e.target.value })} maxLength={5000} /> : <div className="text-sm whitespace-pre-wrap">{inv.notes || '—'}</div>}
      </Field>
      {dirty && <p className="text-xs text-amber-600">Unsaved changes. Save, or issue (which saves first).</p>}
      <ErrorText>{error}</ErrorText>

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
      {cancelling && <CancelForm invoice={inv} onClose={() => setCancelling(false)} onDone={() => { setCancelling(false); load(); }} />}
    </div>
  );
};

// ---- all invoices ----
export const InvoicesPage = ({ arg }) => {
  const { can } = useAuth();
  const canManage = can('manage_billing');
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  const [status, setStatus] = useState('');
  const [month, setMonth] = useState('');
  const [pay, setPay] = useState('');
  const [companyId, setCompanyId] = useState('');
  const companies = useCompanies();
  const [page, setPage] = useState(1);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [dialog, setDialog] = useState(null); // 'generate' | 'new'
  const [notice, setNotice] = useState('');
  const [issuing, setIssuing] = useState('');
  const [reload, setReload] = useState(0);

  useEffect(() => { const t = setTimeout(() => setDebounced(q.trim()), 300); return () => clearTimeout(t); }, [q]);
  useEffect(() => { setPage(1); }, [debounced, status, month, pay, companyId]);
  useEffect(() => {
    const ctrl = new AbortController();
    setError('');
    api(`/api/invoices${qs({ q: debounced, status, month, pay, company_id: companyId, page })}`, { signal: ctrl.signal })
      .then(setData).catch(err => { if (err.name !== 'AbortError') setError(err.message); });
    return () => ctrl.abort();
  }, [debounced, status, month, pay, companyId, page, reload]);

  const openId = /^\d+$/.test(arg || '') ? arg : null;
  if (openId) {
    return (
      <>
        <button type="button" onClick={() => navigate('invoices')} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline mb-4 cursor-pointer">← All invoices</button>
        <InvoiceDetail key={openId} id={openId} />
      </>
    );
  }

  // Issue every draft of one run (or every manual draft).
  const issueGroup = async (g) => {
    const key = g.billing_month || 'manual';
    const label = g.billing_month ? `${fmtMonth(g.billing_month)} drafts` : 'manual drafts';
    if (!window.confirm(`Issue all ${g.count} ${label} (total ${formatMoney(g.total)})? Each gets its number and can't be changed after this.`)) return;
    setIssuing(key);
    setNotice('');
    setError('');
    try {
      const ids = [];
      for (let p = 1; p <= 50; p += 1) {
        const list = await api(`/api/invoices${qs({ status: 'draft', billing_month: g.billing_month ? monthISO(g.billing_month) : '', page_size: 200, page: p })}`);
        list.invoices.filter(v => (v.billing_month ? dayOf(v.billing_month) : '') === (g.billing_month ? dayOf(g.billing_month) : '')).forEach(v => ids.push(v.id));
        if (p * 200 >= list.total) break;
      }
      const d = await api('/api/invoices/issue', { method: 'POST', body: { ids } });
      const range = d.issued.length ? ` (${d.issued[0]}${d.issued.length > 1 ? ` – ${d.issued[d.issued.length - 1]}` : ''})` : '';
      setNotice(`Issued ${d.issued.length} invoice${d.issued.length === 1 ? '' : 's'}${range}.`
        + (d.skipped.length ? ` Not issued: ${d.skipped.map(s => `${s.name} (${s.reason})`).join(', ')}.` : ''));
      setReload(r => r + 1);
    } catch (err) {
      setError(err.message);
    } finally {
      setIssuing('');
    }
  };

  return (
    <>
      <PageHeader icon={FileText} title="Invoices" subtitle="Monthly runs, manual invoices, printing."
        actions={canManage && <>
          <Button variant="secondary" icon={Plus} onClick={() => setDialog('new')}>New invoice</Button>
          <Button icon={CalendarPlus} onClick={() => setDialog('generate')}>Make monthly invoices</Button>
        </>} />

      {data?.drafts?.length > 0 && (
        <Card className="p-3 mb-4 border-amber-300 dark:border-amber-800 bg-amber-50/60 dark:bg-amber-950/20 space-y-2">
          {data.drafts.map(g => (
            <div key={g.billing_month || 'manual'} className="flex flex-wrap items-center justify-between gap-2 text-sm">
              <span><b>{g.count}</b> {g.billing_month ? `${fmtMonth(g.billing_month)}` : 'manual'} draft{g.count === 1 ? '' : 's'} waiting, total {formatMoney(g.total)}.
                <button type="button" className="ml-2 text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer" onClick={() => { setStatus('draft'); setMonth(''); }}>Review</button>
              </span>
              {canManage && <Button icon={Send} busy={issuing === (g.billing_month || 'manual')} onClick={() => issueGroup(g)}>Issue all {g.count}</Button>}
            </div>
          ))}
        </Card>
      )}
      {notice && <Card className="p-3 mb-4 text-sm border-emerald-300 dark:border-emerald-800 bg-emerald-50/60 dark:bg-emerald-950/20">{notice}</Card>}

      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="relative flex-1 min-w-[14rem] max-w-md">
          <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Invoice no., client name or number…" className={`${inputCls} pl-8`} />
        </span>
        <Select value={status} onChange={(e) => setStatus(e.target.value)} className="!w-auto">
          <option value="">Drafts & issued</option>
          <option value="draft">Drafts</option>
          <option value="issued">Issued</option>
          <option value="cancelled">Cancelled</option>
          <option value="all">All</option>
        </Select>
        <Select value={pay} onChange={(e) => setPay(e.target.value)} className="!w-auto">
          <option value="">Paid & unpaid</option>
          <option value="open">Not fully paid</option>
          <option value="overdue">Overdue</option>
          <option value="paid">Paid</option>
        </Select>
        {companies.length > 1 && <CompanySelect companies={companies} value={companyId} allLabel="All companies" onChange={setCompanyId} className="!w-auto" />}
        <Input type="month" value={month} onChange={(e) => setMonth(e.target.value)} className="!w-auto" title="Invoice date month" />
        {month && <Button variant="ghost" onClick={() => setMonth('')}>All months</Button>}
        {data && <span className="text-sm text-zinc-500">{data.total} invoice{data.total === 1 ? '' : 's'} · {formatMoney(data.amount)}</span>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data && !error ? <Spinner /> : data && (
        <>
          <InvoicesTable invoices={data.invoices}
            empty={debounced || status || month || pay || companyId ? 'No invoices match.' : canManage ? 'No invoices yet. Use "Make monthly invoices" or "New invoice".' : 'No invoices yet.'} />
          <Pager page={data.page} pageSize={data.page_size} total={data.total} onPage={setPage} />
        </>
      )}
      {dialog === 'generate' && (
        <GenerateForm onClose={() => setDialog(null)} onDone={(d) => {
          setDialog(null);
          setNotice(`Made ${d.created} new draft${d.created === 1 ? '' : 's'}${d.updated ? ` and added to ${d.updated} existing` : ''} for ${fmtMonth(`${d.month}-01`)}. Review them, then "Issue all".`);
          setStatus('draft');
          setReload(r => r + 1);
        }} />
      )}
      {dialog === 'new' && <NewInvoiceForm onClose={() => setDialog(null)} onCreated={(inv) => navigate(`invoices/${inv.id}`)} />}
    </>
  );
};
