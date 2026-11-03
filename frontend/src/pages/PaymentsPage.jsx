import React, { useCallback, useEffect, useState } from 'react';
import {
  Wallet, Plus, Search, CheckCircle2, XCircle, Undo2, Paperclip, ExternalLink, History, ListChecks,
} from 'lucide-react';
import { api, apiBlobURL, qs } from '../lib/api';
import { useAuth, navigate } from '../lib/auth';
import { formatMoney, fmtDay, fmtDateTime, todayISO, PAYMENT_STATUS, PAYMENT_METHODS } from '../lib/format';
import {
  PageHeader, Button, Field, Input, Select, Textarea, Badge, Modal, Pager, Table, Spinner, ErrorText, Card, inputCls,
} from '../components/ui';
import { ClientPicker } from './ServicesPage';

// Payments: record with the slip (Payment Recorder), verify / reject /
// reverse (Payment Verifier). Only verified payments count as paid.
// #/payments lists them; #/payments/12 opens one.

export const PaymentStatus = ({ status }) => {
  const [label, tone] = PAYMENT_STATUS[status] || [status, 'zinc'];
  return <Badge tone={tone}>{label}</Badge>;
};

const num = (v) => (String(v ?? '').trim() === '' ? 0 : Number(String(v).replace(/,/g, '')));
const r2 = (v) => Math.round(v * 100) / 100;

// ---- apply an amount to open invoices ----
const AllocationTable = ({ invoices, alloc, setAlloc, available }) => {
  const applied = r2(Object.values(alloc).reduce((s, v) => s + num(v), 0));
  const fillOldest = () => {
    let left = available;
    const next = {};
    invoices.forEach(v => {
      const take = r2(Math.min(left, v.open));
      if (take > 0) { next[v.id] = String(take); left = r2(left - take); }
    });
    setAlloc(next);
  };
  if (invoices.length === 0) {
    return <p className="text-sm text-zinc-500">No open invoices. The whole amount stays as the client's credit (e.g. against the opening balance or future invoices).</p>;
  }
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
        <span className="text-sm">Applied <b>{formatMoney(applied)}</b> of {formatMoney(available)}
          {applied < available && <span className="text-zinc-500"> · {formatMoney(r2(available - applied))} stays as credit</span>}
          {applied > available && <span className="text-rose-600"> · more than the payment</span>}
        </span>
        <span className="flex gap-2">
          <Button variant="secondary" onClick={fillOldest}>Oldest first</Button>
          <Button variant="ghost" onClick={() => setAlloc({})}>Clear</Button>
        </span>
      </div>
      <div className="max-h-64 overflow-y-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
        <table className="w-full text-sm">
          <thead className="bg-zinc-50 dark:bg-zinc-800/60 text-xs text-zinc-600 dark:text-zinc-300">
            <tr><th className="px-2 py-1.5 text-left">Invoice</th><th className="px-2 py-1.5 text-left">Due</th><th className="px-2 py-1.5 text-right">Open</th><th className="px-2 py-1.5 text-right w-36">Apply</th></tr>
          </thead>
          <tbody>
            {invoices.map(v => (
              <tr key={v.id} className="border-t border-zinc-100 dark:border-zinc-800">
                <td className="px-2 py-1.5"><span className="font-mono">{v.invoice_number}</span>
                  <div className="text-[11px] text-zinc-500">{formatMoney(v.total)}{v.pending > 0 && ` · ${formatMoney(v.pending)} in other payments waiting`}</div></td>
                <td className={`px-2 py-1.5 whitespace-nowrap ${v.due_date.slice(0, 10) < todayISO() ? 'text-rose-600' : ''}`}>{fmtDay(v.due_date)}</td>
                <td className="px-2 py-1.5 text-right whitespace-nowrap">{formatMoney(v.open)}</td>
                <td className="px-2 py-1.5">
                  <Input inputMode="decimal" value={alloc[v.id] || ''} placeholder="0" className="text-right !py-1"
                    onChange={(e) => setAlloc(a => ({ ...a, [v.id]: e.target.value }))} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};

const allocList = (alloc) => Object.entries(alloc).map(([id, v]) => ({ invoice_id: Number(id), amount: num(v) })).filter(a => a.amount > 0);

// ---- record a payment ----
export const RecordPaymentForm = ({ client: preset, invoiceId, onClose, onSaved }) => {
  const [client, setClient] = useState(preset || null);
  const [form, setForm] = useState({ payment_date: todayISO(), method: 'bank_transfer', reference: '', bank_name: '', amount: '', tax_deducted: '', notes: '' });
  const [slip, setSlip] = useState(null);
  const [invoices, setInvoices] = useState(null);
  const [alloc, setAlloc] = useState({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [duplicate, setDuplicate] = useState('');
  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.value }));
  const available = r2(num(form.amount) + num(form.tax_deducted));

  useEffect(() => {
    setInvoices(null);
    setAlloc({});
    if (!client) return;
    api(`/api/payments/open-invoices?client_id=${client.id}`).then(d => setInvoices(d.invoices)).catch(err => setError(err.message));
  }, [client]);
  // Opened from an invoice: apply the payment to it by default.
  useEffect(() => {
    if (!invoiceId || !invoices) return;
    const v = invoices.find(x => x.id === Number(invoiceId));
    if (v && available > 0) setAlloc({ [v.id]: String(Math.min(available, v.open)) });
  }, [invoiceId, invoices, available]);

  const save = async (confirmDuplicate = false) => {
    setError('');
    if (!client) { setError('Choose the client who paid'); return; }
    if (!(available > 0)) { setError('Enter the amount received'); return; }
    if (!slip && form.method !== 'cash') { setError('Attach the slip'); return; }
    if (slip && slip.size > 8 * 1024 * 1024) { setError('The slip is too big (max 8 MB)'); return; }
    const fd = new FormData();
    fd.append('data', JSON.stringify({
      ...form, client_id: client.id, amount: num(form.amount), tax_deducted: num(form.tax_deducted),
      allocations: allocList(alloc), confirm_duplicate: confirmDuplicate,
    }));
    if (slip) fd.append('slip', slip);
    setBusy(true);
    try {
      const d = await api('/api/payments', { method: 'POST', form: fd });
      onSaved(d.payment);
    } catch (err) {
      if (err.data?.duplicate) setDuplicate(err.message);
      else setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal wide title="Record payment" onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} onClick={() => save(false)}>Record payment</Button></>}>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <Field label="Client" required className="sm:col-span-3">
          {preset ? <div className={`${inputCls} bg-zinc-50 dark:bg-zinc-900/60`}><b>{client.name}</b> <span className="font-mono text-xs text-zinc-500">{client.client_number}</span></div>
            : <ClientPicker value={client} onChange={setClient} />}
        </Field>
        <Field label="Payment date" required><Input type="date" value={form.payment_date} max={todayISO()} onChange={set('payment_date')} /></Field>
        <Field label="Method" required>
          <Select value={form.method} onChange={set('method')}>
            {Object.entries(PAYMENT_METHODS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
          </Select>
        </Field>
        <Field label={form.method === 'cheque' ? 'Cheque number' : 'Reference / transaction ID'} required={form.method === 'cheque'}>
          <Input value={form.reference} onChange={set('reference')} maxLength={100} />
        </Field>
        <Field label="Amount received (PKR)" required>
          <Input inputMode="decimal" value={form.amount} onChange={set('amount')} maxLength={20} placeholder="0" />
        </Field>
        <Field label="Tax deducted by client (WHT)" hint="If they withheld income tax and paid it for you. Counts as paid.">
          <Input inputMode="decimal" value={form.tax_deducted} onChange={set('tax_deducted')} maxLength={20} placeholder="0" />
        </Field>
        <Field label="Their bank"><Input value={form.bank_name} onChange={set('bank_name')} maxLength={100} placeholder="e.g. HBL" /></Field>
        <Field label="Slip" required={form.method !== 'cash'} className="sm:col-span-3" hint="Photo or PDF of the deposit slip, cheque or transfer receipt (max 8 MB).">
          <input type="file" accept="image/jpeg,image/png,image/webp,application/pdf"
            onChange={(e) => setSlip(e.target.files?.[0] || null)}
            className="block w-full text-sm file:mr-3 file:px-3 file:py-1.5 file:rounded-lg file:border-0 file:bg-indigo-50 file:text-indigo-700 dark:file:bg-indigo-950 dark:file:text-indigo-300 file:font-semibold file:cursor-pointer" />
        </Field>
        <Field label="Notes" className="sm:col-span-3"><Textarea rows={2} value={form.notes} onChange={set('notes')} maxLength={2000} /></Field>
      </div>
      {client && (
        <div className="mt-4">
          <h3 className="text-sm font-semibold mb-2">Apply to invoices</h3>
          {!invoices ? <Spinner /> : <AllocationTable invoices={invoices} alloc={alloc} setAlloc={setAlloc} available={available} />}
        </div>
      )}
      {duplicate && (
        <div className="mt-3 p-3 rounded-lg border border-amber-300 dark:border-amber-800 bg-amber-50 dark:bg-amber-950/30 text-sm text-amber-800 dark:text-amber-300">
          <p>{duplicate}. Record this one too?</p>
          <div className="mt-2 flex gap-2">
            <Button variant="secondary" onClick={() => setDuplicate('')}>Go back</Button>
            <Button onClick={() => { setDuplicate(''); save(true); }}>Yes, record it</Button>
          </div>
        </div>
      )}
      <p className="mt-3 text-xs text-zinc-500">The payment waits for a Payment Verifier. It counts as paid only once verified.</p>
      <div className="mt-2"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- slip ----
const SlipView = ({ payment }) => {
  const [url, setUrl] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    if (!payment.slip_type) return undefined;
    let u = '';
    apiBlobURL(`/api/payments/${payment.id}/slip`).then(x => { u = x; setUrl(x); }).catch(err => setError(err.message));
    return () => { if (u) URL.revokeObjectURL(u); };
  }, [payment.id, payment.slip_type]);
  if (!payment.slip_type) return <p className="text-sm text-zinc-500">No slip (cash).</p>;
  if (error) return <ErrorText>{error}</ErrorText>;
  if (!url) return <Spinner label="Loading slip…" />;
  return (
    <div>
      <div className="flex items-center justify-between gap-2 mb-2 text-xs text-zinc-500">
        <span className="flex items-center gap-1"><Paperclip className="w-3.5 h-3.5" /> {payment.slip_name} · {Math.round(payment.slip_size / 1024)} KB</span>
        <a href={url} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-indigo-600 dark:text-indigo-400 hover:underline">Open full size <ExternalLink className="w-3.5 h-3.5" /></a>
      </div>
      {payment.slip_type === 'application/pdf'
        ? <iframe title="Slip" src={url} className="w-full h-[28rem] rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white" />
        : <img src={url} alt="Payment slip" className="max-h-[28rem] mx-auto rounded-lg border border-zinc-200 dark:border-zinc-800" />}
    </div>
  );
};

const ReasonForm = ({ title, action, intro, onClose, onDone }) => {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const save = async () => {
    setBusy(true);
    setError('');
    try { await action(reason); onDone(); } catch (err) { setError(err.message); } finally { setBusy(false); }
  };
  return (
    <Modal title={title} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Back</Button><Button variant="danger" busy={busy} onClick={save}>{title}</Button></>}>
      <p className="text-sm mb-3">{intro}</p>
      <Field label="Reason" required><Textarea autoFocus value={reason} onChange={(e) => setReason(e.target.value)} maxLength={1000} /></Field>
      <div className="mt-2"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

const ApplyCreditForm = ({ payment, available, onClose, onDone }) => {
  const [invoices, setInvoices] = useState(null);
  const [alloc, setAlloc] = useState({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    api(`/api/payments/open-invoices?client_id=${payment.client_id}`).then(d => setInvoices(d.invoices)).catch(err => setError(err.message));
  }, [payment.client_id]);
  const save = async () => {
    setBusy(true);
    setError('');
    try {
      await api(`/api/payments/${payment.id}/allocate`, { method: 'POST', body: { allocations: allocList(alloc) } });
      onDone();
    } catch (err) { setError(err.message); } finally { setBusy(false); }
  };
  return (
    <Modal wide title={`Apply ${payment.payment_number} credit`} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} onClick={save}>Apply</Button></>}>
      {!invoices ? <Spinner /> : <AllocationTable invoices={invoices} alloc={alloc} setAlloc={setAlloc} available={available} />}
      <div className="mt-2"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- one payment ----
const PaymentDetail = ({ id }) => {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [dialog, setDialog] = useState(null); // reject | reverse | apply

  const load = useCallback(() => {
    setError('');
    api(`/api/payments/${id}`).then(setData).catch(err => setError(err.message));
  }, [id]);
  useEffect(() => { load(); }, [load]);

  if (error && !data) return <ErrorText>{error}</ErrorText>;
  if (!data) return <Spinner />;
  const p = data.payment;
  const verify = async () => {
    if (!window.confirm(`Verify ${p.payment_number}: ${formatMoney(data.credited)} from ${p.client?.name}? Check the slip against the bank first.`)) return;
    setBusy(true);
    setError('');
    try { await api(`/api/payments/${p.id}/verify`, { method: 'POST', body: {} }); load(); } catch (err) { setError(err.message); } finally { setBusy(false); }
  };
  const row = (label, value) => (
    <div><div className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">{label}</div><div className="text-sm break-words whitespace-pre-wrap">{value || '—'}</div></div>
  );

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="font-mono text-xs text-zinc-500">{p.payment_number}</div>
          <h2 className="text-lg font-bold flex items-center gap-2">{formatMoney(data.credited)} <PaymentStatus status={p.status} /></h2>
          <button type="button" onClick={() => navigate(`clients/${p.client_id}`)} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer">
            {p.client?.name} <span className="font-mono text-xs">{p.client?.client_number}</span>
          </button>
        </div>
        {data.can_verify && (
          <div className="flex flex-wrap gap-2">
            {p.status === 'pending' && <>
              <Button icon={CheckCircle2} busy={busy} disabled={data.own_payment} onClick={verify}
                title={data.own_payment ? 'You recorded this payment; someone else has to verify it' : ''}>Verify</Button>
              <Button variant="secondary" icon={XCircle} onClick={() => setDialog('reject')}>Reject</Button>
            </>}
            {p.status === 'verified' && <Button variant="secondary" icon={Undo2} onClick={() => setDialog('reverse')}>Reverse</Button>}
          </div>
        )}
      </div>
      {data.own_payment && p.status === 'pending' && <p className="text-xs text-amber-600">You recorded this payment, so another verifier has to check it.</p>}
      {p.status === 'rejected' && <Card className="p-3 text-sm border-rose-300 dark:border-rose-900 bg-rose-50/60 dark:bg-rose-950/20">Rejected by {p.checked_by}, {fmtDateTime(p.checked_at)}: {p.reject_reason}</Card>}
      {p.status === 'reversed' && <Card className="p-3 text-sm border-zinc-300 dark:border-zinc-700 bg-zinc-100 dark:bg-zinc-900">Reversed by {p.reversed_by}, {fmtDateTime(p.reversed_at)}: {p.reverse_reason}</Card>}
      <ErrorText>{error}</ErrorText>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card className="p-4 grid grid-cols-2 gap-4 content-start">
          {row('Payment date', fmtDay(p.payment_date))}
          {row('Method', PAYMENT_METHODS[p.method])}
          {row('Reference', p.reference)}
          {row('Their bank', p.bank_name)}
          {row('Amount received', formatMoney(p.amount))}
          {row('Tax deducted (WHT)', p.tax_deducted ? formatMoney(p.tax_deducted) : '—')}
          {row('Recorded by', `${p.recorded_by}, ${fmtDateTime(p.created_at)}`)}
          {row(p.status === 'rejected' ? 'Rejected by' : 'Verified by', p.checked_by ? `${p.checked_by}, ${fmtDateTime(p.checked_at)}` : 'Not yet')}
          {p.notes && <div className="col-span-2">{row('Notes', p.notes)}</div>}
        </Card>
        <Card className="p-4"><SlipView payment={p} /></Card>
      </div>

      <div>
        <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
          <h3 className="text-sm font-semibold flex items-center gap-1.5"><ListChecks className="w-4 h-4 text-indigo-500" /> Applied to invoices</h3>
          {p.status === 'verified' && data.unallocated > 0 && data.can_allocate &&
            <Button variant="secondary" onClick={() => setDialog('apply')}>Apply {formatMoney(data.unallocated)} credit</Button>}
        </div>
        <Table head={['Invoice', 'Date', { label: 'Invoice total', right: true }, { label: 'Applied', right: true }]}>
          {(p.allocations || []).length === 0 && <tr><td colSpan={4} className="px-3 py-4 text-center text-zinc-500">Not applied to any invoice.</td></tr>}
          {(p.allocations || []).map(a => (
            <tr key={a.id} onClick={() => navigate(`invoices/${a.invoice_id}`)} className="border-t border-zinc-100 dark:border-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer">
              <td className="px-3 py-2 font-mono">{a.invoice?.invoice_number}</td>
              <td className="px-3 py-2">{fmtDay(a.invoice?.invoice_date)}</td>
              <td className="px-3 py-2 text-right">{formatMoney(a.invoice?.total)}</td>
              <td className="px-3 py-2 text-right font-semibold">{formatMoney(a.amount)}</td>
            </tr>
          ))}
        </Table>
        {data.unallocated > 0 && <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">Not applied: <b>{formatMoney(data.unallocated)}</b> (client's credit).</p>}
      </div>

      <div>
        <h3 className="text-sm font-semibold mb-2 flex items-center gap-1.5"><History className="w-4 h-4 text-indigo-500" /> History</h3>
        <ol className="space-y-1.5">
          {data.history.map(h => (
            <li key={h.id} className="text-xs text-zinc-700 dark:text-zinc-300">
              <span className="text-zinc-500">{fmtDateTime(h.created_at)}</span> · <b>{h.user_name}</b> · {h.action} · {h.details}
            </li>
          ))}
        </ol>
      </div>

      {dialog === 'reject' && (
        <ReasonForm title="Reject payment" intro="The payment won't count. It stays on record as rejected; the client's invoices stay unpaid."
          action={(reason) => api(`/api/payments/${p.id}/reject`, { method: 'POST', body: { reason } })}
          onClose={() => setDialog(null)} onDone={() => { setDialog(null); load(); }} />
      )}
      {dialog === 'reverse' && (
        <ReasonForm title="Reverse payment" intro="For a bounced cheque or a transfer that was returned. The invoices it paid become unpaid again."
          action={(reason) => api(`/api/payments/${p.id}/reverse`, { method: 'POST', body: { reason } })}
          onClose={() => setDialog(null)} onDone={() => { setDialog(null); load(); }} />
      )}
      {dialog === 'apply' && <ApplyCreditForm payment={p} available={data.unallocated} onClose={() => setDialog(null)} onDone={() => { setDialog(null); load(); }} />}
    </div>
  );
};

// ---- the list (all, or one client's) ----
export const PaymentsTable = ({ payments, showClient = true, empty }) => (
  <Table head={['Payment', ...(showClient ? ['Client'] : []), 'Method', { label: 'Amount', right: true }, 'Status']}>
    {payments.length === 0 && <tr><td colSpan={showClient ? 5 : 4} className="px-3 py-6 text-center text-zinc-500">{empty}</td></tr>}
    {payments.map(p => (
      <tr key={p.id} onClick={() => navigate(`payments/${p.id}`)} className="border-t border-zinc-100 dark:border-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer">
        <td className="px-3 py-2 whitespace-nowrap"><div className="font-mono font-semibold">{p.payment_number}</div><div className="text-[11px] text-zinc-500">{fmtDay(p.payment_date)}</div></td>
        {showClient && <td className="px-3 py-2 min-w-[10rem]"><div>{p.client?.name}</div><div className="font-mono text-[11px] text-zinc-500">{p.client?.client_number}</div></td>}
        <td className="px-3 py-2"><div className="whitespace-nowrap">{PAYMENT_METHODS[p.method]}</div><div className="text-[11px] text-zinc-500">{[p.reference, p.bank_name].filter(Boolean).join(' · ')}</div></td>
        <td className="px-3 py-2 text-right whitespace-nowrap"><div className="font-semibold">{formatMoney(p.amount)}</div>{p.tax_deducted > 0 && <div className="text-[11px] text-zinc-500">+ WHT {formatMoney(p.tax_deducted)}</div>}</td>
        <td className="px-3 py-2"><PaymentStatus status={p.status} /></td>
      </tr>
    ))}
  </Table>
);

export const ClientPayments = ({ client }) => {
  const { can } = useAuth();
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);
  useEffect(() => {
    api(`/api/payments${qs({ client_id: client.id, page_size: 100 })}`).then(setData).catch(err => setError(err.message));
  }, [client.id]);
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
        <h3 className="text-sm font-semibold flex items-center gap-1.5"><Wallet className="w-4 h-4 text-indigo-500" /> Payments</h3>
        {can('record_payments') && <Button variant="secondary" icon={Plus} onClick={() => setAdding(true)}>Record payment</Button>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data ? (!error && <Spinner />) : <PaymentsTable showClient={false} payments={data.payments} empty="No payments yet." />}
      {adding && <RecordPaymentForm client={client} onClose={() => setAdding(false)} onSaved={(p) => navigate(`payments/${p.id}`)} />}
    </div>
  );
};

// ---- all payments ----
export const PaymentsPage = ({ arg, query }) => {
  const { can } = useAuth();
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  const [status, setStatus] = useState(() => (can('verify_payments') ? 'pending' : ''));
  const [method, setMethod] = useState('');
  const [month, setMonth] = useState('');
  const [page, setPage] = useState(1);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);

  useEffect(() => { const t = setTimeout(() => setDebounced(q.trim()), 300); return () => clearTimeout(t); }, [q]);
  useEffect(() => { setPage(1); }, [debounced, status, method, month]);
  useEffect(() => {
    const ctrl = new AbortController();
    setError('');
    api(`/api/payments${qs({ q: debounced, status, method, month, page })}`, { signal: ctrl.signal })
      .then(setData).catch(err => { if (err.name !== 'AbortError') setError(err.message); });
    return () => ctrl.abort();
  }, [debounced, status, method, month, page]);
  // Opened as #/payments?record=1&client=3&invoice=9 (from an invoice).
  const [preset, setPreset] = useState(null);
  useEffect(() => {
    const cid = query?.get('client');
    if (query?.get('record') && cid) {
      api(`/api/clients/${cid}`).then(d => { setPreset({ client: d.client, invoice: query.get('invoice') }); setAdding(true); }).catch(() => {});
    }
  }, [query]);

  const openId = /^\d+$/.test(arg || '') ? arg : null;
  if (openId) {
    return (
      <>
        <button type="button" onClick={() => (window.history.length > 1 ? window.history.back() : navigate('payments'))} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline mb-4 cursor-pointer">← Back</button>
        <PaymentDetail key={openId} id={openId} />
      </>
    );
  }

  return (
    <>
      <PageHeader icon={Wallet} title="Payments" subtitle="Recorded with the slip; counted once verified."
        actions={can('record_payments') && <Button icon={Plus} onClick={() => { setPreset(null); setAdding(true); }}>Record payment</Button>} />
      {data?.pending?.count > 0 && status !== 'pending' && (
        <Card className="p-3 mb-4 text-sm border-amber-300 dark:border-amber-800 bg-amber-50/60 dark:bg-amber-950/20 flex flex-wrap items-center justify-between gap-2">
          <span><b>{data.pending.count}</b> payment{data.pending.count === 1 ? '' : 's'} waiting for verification ({formatMoney(data.pending.amount)}).</span>
          <Button variant="secondary" onClick={() => setStatus('pending')}>Show them</Button>
        </Card>
      )}
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="relative flex-1 min-w-[14rem] max-w-md">
          <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="PAY no., reference, client…" className={`${inputCls} pl-8`} />
        </span>
        <Select value={status} onChange={(e) => setStatus(e.target.value)} className="!w-auto">
          <option value="">All statuses</option>
          <option value="pending">Waiting for verification</option>
          <option value="verified">Verified</option>
          <option value="rejected">Rejected</option>
          <option value="reversed">Reversed</option>
        </Select>
        <Select value={method} onChange={(e) => setMethod(e.target.value)} className="!w-auto">
          <option value="">All methods</option>
          {Object.entries(PAYMENT_METHODS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </Select>
        <Input type="month" value={month} onChange={(e) => setMonth(e.target.value)} className="!w-auto" title="Payment date month" />
        {month && <Button variant="ghost" onClick={() => setMonth('')}>All months</Button>}
        {data && <span className="text-sm text-zinc-500">{data.total} · {formatMoney(data.amount)}{data.tax > 0 ? ` + WHT ${formatMoney(data.tax)}` : ''}</span>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data && !error ? <Spinner /> : data && (
        <>
          <PaymentsTable payments={data.payments} empty={status === 'pending' ? 'Nothing waiting for verification.' : 'No payments match.'} />
          <Pager page={data.page} pageSize={data.page_size} total={data.total} onPage={setPage} />
        </>
      )}
      {adding && (
        <RecordPaymentForm client={preset?.client} invoiceId={preset?.invoice}
          onClose={() => { setAdding(false); if (preset) navigate('payments'); }} onSaved={(p) => navigate(`payments/${p.id}`)} />
      )}
    </>
  );
};
