import React, { useCallback, useEffect, useState } from 'react';
import {
  Handshake, Plus, Search, Pencil, History, RefreshCw, Ban, Upload, Paperclip, Link2, Unlink, AlertTriangle, ShieldCheck,
} from 'lucide-react';
import { api, apiBlobURL, qs } from '../lib/api';
import { useAuth, navigate } from '../lib/auth';
import {
  formatMoney, fmtDay, fmtDateTime, dayOf, todayISO, CONTRACT_STATES, CHARGE_TYPES, slaTargets,
} from '../lib/format';
import {
  PageHeader, Button, Field, Input, Select, Textarea, Badge, Modal, Pager, Table, Spinner, ErrorText, Card, inputCls,
} from '../components/ui';
import { ClientPicker, ServiceForm, ServicesTable, StateBadge } from './ServicesPage';

// Client contracts: the agreement (term, renewal, notice, default service
// level, signed copy) and the services under it. #/contracts lists them,
// #/contracts/12 opens one, each client's page shows its own.

export const ContractBadge = ({ state }) => {
  const [label, tone] = CONTRACT_STATES[state] || [state, 'zinc'];
  return <Badge tone={tone}>{label}</Badge>;
};

const termText = (k) => {
  if (!k.end_date) return `${fmtDay(k.start_date)} → no fixed end`;
  return `${fmtDay(k.start_date)} → ${fmtDay(k.term_end || k.end_date)}`;
};

// What to do / know about the contract's term, in one line.
const termNote = (k) => {
  switch (k.state) {
    case 'due':
      return k.auto_renew
        ? `Renews by itself on ${fmtDay(k.term_end)} for ${k.renew_months} months. Notice date was ${fmtDay(k.notice_by)}.`
        : `Ends ${fmtDay(k.term_end)}. Notice date ${fmtDay(k.notice_by)} reached: renew or let it end.`;
    case 'expired': return `Ended ${fmtDay(k.term_end)} and was not renewed.`;
    case 'terminated': return `Terminated, last day ${fmtDay(k.terminated_on)}.`;
    case 'upcoming': return `Starts ${fmtDay(k.start_date)}.`;
    case 'draft': return 'Draft: not signed / in force yet.';
    default:
      if (!k.end_date) return 'No fixed end date.';
      return k.auto_renew
        ? `Renews every ${k.renew_months} months${k.renewals ? ` (renewed ${k.renewals}×)` : ''}. Notice by ${fmtDay(k.notice_by)} to stop the next renewal.`
        : `Ends ${fmtDay(k.term_end)}. Notice by ${fmtDay(k.notice_by)}.`;
  }
};

const EMPTY = {
  title: '', client_reference: '', signed_date: '', start_date: todayISO(), end_date: '', auto_renew: false,
  renew_months: '12', notice_days: '30', sla_plan_id: '', status: 'active', notes: '',
};

const fromContract = (k) => ({
  title: k.title, client_reference: k.client_reference || '', signed_date: dayOf(k.signed_date), start_date: dayOf(k.start_date),
  end_date: dayOf(k.end_date), auto_renew: !!k.auto_renew, renew_months: String(k.renew_months || 12),
  notice_days: String(k.notice_days ?? 30), sla_plan_id: k.sla_plan_id ? String(k.sla_plan_id) : '', status: k.status, notes: k.notes || '',
});

// "12 months from the start" quick fill for the end date.
const plusMonths = (iso, months) => {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(iso || '')) return '';
  const [y, m, d] = iso.split('-').map(Number);
  const end = new Date(y, m - 1 + months, d);
  end.setDate(end.getDate() - 1); // a 12-month term from 1 Jan ends 31 Dec
  return `${end.getFullYear()}-${String(end.getMonth() + 1).padStart(2, '0')}-${String(end.getDate()).padStart(2, '0')}`;
};

// ---- add / edit a contract ----
const ContractForm = ({ contract, client: presetClient, onClose, onSaved }) => {
  const [client, setClient] = useState(presetClient || contract?.client || null);
  const [plans, setPlans] = useState([]);
  const [form, setForm] = useState(() => (contract ? fromContract(contract) : EMPTY));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));

  useEffect(() => { api('/api/sla-plans?status=all').then(d => setPlans(d.plans)).catch(() => {}); }, []);
  const planChoices = plans.filter(p => p.active || String(p.id) === form.sla_plan_id);
  const chosenPlan = plans.find(p => String(p.id) === form.sla_plan_id);

  const save = async () => {
    setError('');
    if (!contract && !client) { setError('Choose a client'); return; }
    if (!form.title.trim()) { setError('Give the contract a title'); return; }
    setBusy(true);
    try {
      const body = {
        ...form, client_id: client?.id, renew_months: Number(form.renew_months) || 12,
        notice_days: Number(form.notice_days) || 0, sla_plan_id: Number(form.sla_plan_id) || 0,
      };
      const d = contract
        ? await api(`/api/contracts/${contract.id}`, { method: 'PUT', body })
        : await api('/api/contracts', { method: 'POST', body });
      onSaved(d.contract);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal wide title={contract ? `Edit ${contract.contract_number}` : 'New contract'} onClose={onClose}
      footer={<>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button busy={busy} onClick={save}>{contract ? 'Save changes' : 'Add contract'}</Button>
      </>}>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <Field label="Client" required className="sm:col-span-3">
          {contract || presetClient
            ? <div className={`${inputCls} bg-zinc-50 dark:bg-zinc-900/60`}><b>{client?.name}</b> <span className="font-mono text-xs text-zinc-500">{client?.client_number}</span></div>
            : <ClientPicker value={client} onChange={setClient} />}
        </Field>
        <Field label="Title" required className="sm:col-span-2" hint='e.g. "MPLS connectivity agreement – 12 branches".'>
          <Input autoFocus={!!contract || !!presetClient} value={form.title} onChange={set('title')} maxLength={200} />
        </Field>
        <Field label="Client's reference" hint="Their PO / agreement number.">
          <Input value={form.client_reference} onChange={set('client_reference')} maxLength={100} />
        </Field>
        <Field label="Start date" required>
          <Input type="date" value={form.start_date} onChange={set('start_date')} />
        </Field>
        <Field label="End date" hint={<>Last day of the (first) term. Empty = no fixed end.{' '}
          {form.start_date && [12, 24, 36].map(m => (
            <button key={m} type="button" onClick={() => setForm(f => ({ ...f, end_date: plusMonths(f.start_date, m) }))}
              className="text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer mr-1.5">{m / 12} yr</button>
          ))}</>}>
          <Input type="date" value={form.end_date} onChange={set('end_date')} min={form.start_date || undefined} />
        </Field>
        <Field label="Signed on">
          <Input type="date" value={form.signed_date} onChange={set('signed_date')} max={todayISO()} />
        </Field>
        <Field label="Notice period (days)" hint="Notice needed to end it or stop a renewal.">
          <Input inputMode="numeric" value={form.notice_days} onChange={set('notice_days')} maxLength={3} />
        </Field>
        <div className="sm:col-span-2 flex flex-wrap items-end gap-3">
          <label className="flex items-center gap-2 text-sm py-2">
            <input type="checkbox" checked={form.auto_renew} onChange={set('auto_renew')} /> Renews by itself at the end of each term
          </label>
          <Field label="Each renewal (months)">
            <Input inputMode="numeric" value={form.renew_months} onChange={set('renew_months')} maxLength={3} className="!w-24" />
          </Field>
        </div>
        <Field label="Default service level (SLA)" className="sm:col-span-2" hint={chosenPlan ? `${slaTargets(chosenPlan)}. Filled in on services added to this contract.` : 'Filled in on services added to this contract. Each service can still have its own.'}>
          <Select value={form.sla_plan_id} onChange={set('sla_plan_id')}>
            <option value="">None</option>
            {planChoices.map(p => <option key={p.id} value={p.id}>{p.name}{p.active ? '' : ' — inactive'}</option>)}
          </Select>
        </Field>
        <Field label="Status" hint={contract?.status === 'active' ? 'Active contracts stay active (terminate to end one).' : 'Draft until signed.'}>
          <Select value={form.status} onChange={set('status')} disabled={contract?.status === 'active'}>
            <option value="draft">Draft</option>
            <option value="active">Active</option>
          </Select>
        </Field>
        <Field label="Notes" className="sm:col-span-3"><Textarea rows={2} value={form.notes} onChange={set('notes')} maxLength={5000} /></Field>
      </div>
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- renew ----
const RenewForm = ({ contract: k, onClose, onSaved }) => {
  const [months, setMonths] = useState(String(k.renew_months || 12));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const from = dayOf(k.term_end || k.end_date);
  const save = async () => {
    setBusy(true);
    setError('');
    try {
      await api(`/api/contracts/${k.id}/renew`, { method: 'POST', body: { months: Number(months) || 0 } });
      onSaved();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  // The same rule as the server: whole months added to the current term
  // end, a month-end staying a month-end (31 Jan + 1 → 28/29 Feb).
  const n = Number(months) || 0;
  let newEnd = '—';
  if (n > 0 && from) {
    const [y, m, d] = from.split('-').map(Number);
    const first = new Date(y, m - 1 + n, 1);
    const last = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate();
    newEnd = fmtDay(`${first.getFullYear()}-${String(first.getMonth() + 1).padStart(2, '0')}-${String(Math.min(d, last)).padStart(2, '0')}`);
  }
  return (
    <Modal title={`Renew ${k.contract_number}`} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} icon={RefreshCw} onClick={save}>Renew</Button></>}>
      <Field label="Renew for (months)" required>
        <Input inputMode="numeric" value={months} onChange={(e) => setMonths(e.target.value)} maxLength={3} />
      </Field>
      <p className="mt-3 text-sm">Current term ends <b>{fmtDay(from)}</b>; after renewing it ends <b>{newEnd}</b>.</p>
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- terminate ----
const TerminateForm = ({ contract: k, current, onClose, onSaved }) => {
  const [date, setDate] = useState(dayOf(k.term_end) && dayOf(k.term_end) >= todayISO() ? dayOf(k.term_end) : todayISO());
  const [reason, setReason] = useState('');
  const [endServices, setEndServices] = useState(current > 0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const save = async () => {
    if (!reason.trim()) { setError('Give the reason'); return; }
    setBusy(true);
    setError('');
    try {
      await api(`/api/contracts/${k.id}/terminate`, { method: 'POST', body: { date, reason, end_services: endServices } });
      onSaved();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal title={`Terminate ${k.contract_number}`} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="danger" busy={busy} icon={Ban} onClick={save}>Terminate</Button></>}>
      <div className="space-y-3">
        <Field label="Last day of the contract" required>
          <Input type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        </Field>
        <Field label="Reason" required hint="e.g. client gave notice on 15 Oct; not renewed.">
          <Textarea rows={2} value={reason} onChange={(e) => setReason(e.target.value)} maxLength={2000} />
        </Field>
        {current > 0 && (
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1" checked={endServices} onChange={(e) => setEndServices(e.target.checked)} />
            <span>Also end its {current} current service{current === 1 ? '' : 's'} on that day (billed up to and including it; part month by days).</span>
          </label>
        )}
        <p className="text-xs text-zinc-500">A terminated contract can't be changed again. Nothing is deleted.</p>
      </div>
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- put existing services under the contract ----
const LinkServicesForm = ({ contract: k, onClose, onSaved }) => {
  const [list, setList] = useState(null);
  const [picked, setPicked] = useState({});
  const [applySla, setApplySla] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    api(`/api/subscriptions${qs({ client_id: k.client_id, state: 'current', page_size: 200 })}`)
      .then(d => setList(d.services.filter(s => s.contract_id !== k.id))).catch(err => setError(err.message));
  }, [k.id, k.client_id]);
  const ids = Object.keys(picked).filter(id => picked[id]).map(Number);
  const save = async () => {
    setBusy(true);
    setError('');
    try {
      await api(`/api/contracts/${k.id}/services`, { method: 'POST', body: { add: ids, apply_sla: applySla } });
      onSaved();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal wide title={`Add ${k.client?.name || 'client'}'s services to ${k.contract_number}`} onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button busy={busy} disabled={ids.length === 0} icon={Link2} onClick={save}>Add {ids.length || ''} service{ids.length === 1 ? '' : 's'}</Button></>}>
      {!list ? (!error && <Spinner />) : list.length === 0 ? <p className="text-sm text-zinc-500">All of this client's current services are already on this contract.</p> : (
        <div className="space-y-1 max-h-[50vh] overflow-y-auto">
          {list.map(s => (
            <label key={s.id} className="flex items-start gap-2.5 p-2 rounded-lg hover:bg-zinc-50 dark:hover:bg-zinc-900 cursor-pointer">
              <input type="checkbox" className="mt-1" checked={!!picked[s.id]} onChange={(e) => setPicked(p => ({ ...p, [s.id]: e.target.checked }))} />
              <span className="flex-1 min-w-0 text-sm">
                <b>{s.description}</b> <StateBadge state={s.state} />
                <span className="block text-[11px] text-zinc-500">
                  <span className="font-mono">{s.subscription_number}</span>{s.reference && ` · ${s.reference}`} · {CHARGE_TYPES[s.charge_type]} {formatMoney(s.amount)}
                  {s.sla_plan && ` · SLA ${s.sla_plan.name}`}
                  {s.contract && <span className="text-amber-600"> · now on {s.contract.contract_number} (will move)</span>}
                </span>
              </span>
            </label>
          ))}
        </div>
      )}
      {k.sla_plan && list?.length > 0 && (
        <label className="mt-3 flex items-center gap-2 text-sm">
          <input type="checkbox" checked={applySla} onChange={(e) => setApplySla(e.target.checked)} /> Give services that have no SLA this contract's default ({k.sla_plan.name})
        </label>
      )}
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
    </Modal>
  );
};

// ---- signed copy ----
const DocumentViewer = ({ contract: k, onClose }) => {
  const [url, setUrl] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    let u = '';
    apiBlobURL(`/api/contracts/${k.id}/document`).then(x => { u = x; setUrl(x); }).catch(err => setError(err.message));
    return () => { if (u) URL.revokeObjectURL(u); };
  }, [k.id]);
  return (
    <Modal wide title={`${k.contract_number} · signed copy`} onClose={onClose}>
      {error ? <ErrorText>{error}</ErrorText> : !url ? <Spinner label="Loading…" /> : (
        <div>
          <div className="flex items-center justify-between gap-2 mb-2 text-xs text-zinc-500">
            <span className="flex items-center gap-1"><Paperclip className="w-3.5 h-3.5" /> {k.doc_name} · {Math.round(k.doc_size / 1024)} KB</span>
            <a href={url} target="_blank" rel="noreferrer" className="text-indigo-600 dark:text-indigo-400 hover:underline">Open full size</a>
          </div>
          {k.doc_type === 'application/pdf'
            ? <iframe title="Signed contract" src={url} className="w-full h-[70vh] rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white" />
            : <img src={url} alt="Signed contract" className="max-h-[70vh] mx-auto rounded-lg border border-zinc-200 dark:border-zinc-800" />}
        </div>
      )}
    </Modal>
  );
};

const DocumentBox = ({ contract: k, canChange, onChanged }) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [viewing, setViewing] = useState(false);
  const open = () => setViewing(true);
  const upload = async (e) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    if (file.size > 20 * 1024 * 1024) { setError('The file is too big (max 20 MB)'); return; }
    setBusy(true);
    setError('');
    try {
      const fd = new FormData();
      fd.append('file', file);
      await api(`/api/contracts/${k.id}/document`, { method: 'POST', form: fd });
      onChanged();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div>
      <div className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">Signed copy</div>
      <div className="flex flex-wrap items-center gap-2 mt-0.5">
        {k.doc_name
          ? <button type="button" onClick={open} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer inline-flex items-center gap-1"><Paperclip className="w-3.5 h-3.5" /> {k.doc_name}</button>
          : <span className="text-sm text-zinc-500">Not attached</span>}
        {canChange && (
          <label className={`inline-flex items-center gap-1 text-xs font-semibold text-indigo-600 dark:text-indigo-400 hover:underline ${busy ? 'opacity-60' : 'cursor-pointer'}`}>
            <Upload className="w-3.5 h-3.5" /> {busy ? 'Uploading…' : k.doc_name ? 'Replace' : 'Attach PDF / scan'}
            <input type="file" accept="application/pdf,image/jpeg,image/png,image/webp" className="hidden" onChange={upload} disabled={busy} />
          </label>
        )}
      </div>
      <ErrorText>{error}</ErrorText>
      {viewing && <DocumentViewer contract={k} onClose={() => setViewing(false)} />}
    </div>
  );
};

// ---- one contract ----
const ContractDetail = ({ id }) => {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [dialog, setDialog] = useState(null); // edit | renew | terminate | link | add
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    api(`/api/contracts/${id}`).then(setData).catch(err => setError(err.message));
  }, [id]);
  useEffect(() => { load(); }, [load]);

  if (error && !data) return <ErrorText>{error}</ErrorText>;
  if (!data) return <Spinner />;
  const k = data.contract;
  const open = k.state !== 'terminated';
  const canChange = data.can_manage && open;
  const current = data.services.filter(s => !['ended', 'billed'].includes(s.state));
  const past = data.services.filter(s => ['ended', 'billed'].includes(s.state));

  const unlink = async (s) => {
    if (!window.confirm(`Take ${s.subscription_number} ${s.description} off this contract? The service itself doesn't change.`)) return;
    setBusy(true);
    setError('');
    try {
      await api(`/api/contracts/${k.id}/services`, { method: 'POST', body: { remove: [s.id] } });
      load();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  const activate = async () => {
    setBusy(true);
    setError('');
    try {
      await api(`/api/contracts/${k.id}`, { method: 'PUT', body: { ...fromContract(k), sla_plan_id: k.sla_plan_id || 0, status: 'active' } });
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
          <div className="font-mono text-xs text-zinc-500">{k.contract_number}</div>
          <h2 className="text-lg font-bold flex items-center gap-2">{k.title} <ContractBadge state={k.state} /></h2>
          <button type="button" onClick={() => navigate(`clients/${k.client_id}`)} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer">
            {k.client?.name} <span className="font-mono text-xs">{k.client?.client_number}</span>
          </button>
        </div>
        {canChange && (
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" icon={Pencil} onClick={() => setDialog('edit')}>Edit</Button>
            {k.status === 'draft' && <Button variant="secondary" busy={busy} onClick={activate}>Mark as signed / active</Button>}
            {k.end_date && k.status === 'active' && <Button variant="secondary" icon={RefreshCw} onClick={() => setDialog('renew')}>Renew</Button>}
            <Button variant="secondary" icon={Ban} onClick={() => setDialog('terminate')}>Terminate</Button>
          </div>
        )}
      </div>
      <ErrorText>{error}</ErrorText>
      {(k.state === 'due' || k.state === 'expired') && (
        <Card className="p-3 text-sm border-amber-300 dark:border-amber-800 bg-amber-50/60 dark:bg-amber-950/20 flex items-start gap-2">
          <AlertTriangle className="w-4 h-4 mt-0.5 text-amber-600 shrink-0" /> {termNote(k)}
        </Card>
      )}
      <Card className="p-4 grid grid-cols-1 sm:grid-cols-3 gap-4">
        {row(k.renewals ? 'Current term' : 'Term', termText(k))}
        {row('Renewal', k.end_date ? (k.auto_renew ? `Automatic, every ${k.renew_months} months` : 'Not automatic') : '—')}
        {row('Notice', `${k.notice_days} days${k.notice_by && open ? ` · by ${fmtDay(k.notice_by)}` : ''}`)}
        {row('Signed on', fmtDay(k.signed_date))}
        {row("Client's reference", k.client_reference)}
        <DocumentBox contract={k} canChange={canChange} onChanged={load} />
        <div className="sm:col-span-3">
          <div className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">Default service level</div>
          {k.sla_plan
            ? <div className="text-sm flex items-center gap-1.5"><ShieldCheck className="w-4 h-4 text-indigo-500" /><b>{k.sla_plan.name}</b> <span className="text-zinc-500">{slaTargets(k.sla_plan)}</span></div>
            : <div className="text-sm">None</div>}
        </div>
        {k.state === 'terminated' && <div className="sm:col-span-3">{row('Terminated', `Last day ${fmtDay(k.terminated_on)}. ${k.terminate_reason}`)}</div>}
        {k.notes && <div className="sm:col-span-3">{row('Notes', k.notes)}</div>}
        {open && <div className="sm:col-span-3 text-xs text-zinc-500">{termNote(k)}</div>}
      </Card>

      <div>
        <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
          <h3 className="text-sm font-semibold">Services under this contract</h3>
          {canChange && (
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" icon={Link2} onClick={() => setDialog('link')}>Add existing services</Button>
              <Button icon={Plus} onClick={() => setDialog('add')}>New service</Button>
            </div>
          )}
        </div>
        <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm mb-3">
          <span>Monthly recurring (MRC): <b>{formatMoney(k.monthly)}</b></span>
          {k.annual > 0 && <span>Annual: <b>{formatMoney(k.annual)}</b></span>}
          <span className="text-zinc-500">{k.services} current service{k.services === 1 ? '' : 's'} · before tax</span>
        </div>
        <ServicesTable services={current} empty={canChange ? 'No services yet. Add existing ones or a new one.' : 'No current services.'} />
        {canChange && current.length > 0 && (
          <div className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1">
            {current.map(s => (
              <button key={s.id} type="button" disabled={busy} onClick={() => unlink(s)}
                className="text-[11px] text-zinc-500 hover:text-rose-600 inline-flex items-center gap-1 cursor-pointer"><Unlink className="w-3 h-3" /> take {s.subscription_number} off</button>
            ))}
          </div>
        )}
        {past.length > 0 && (
          <details className="mt-3">
            <summary className="text-xs text-zinc-500 cursor-pointer">{past.length} ended / billed service{past.length === 1 ? '' : 's'}</summary>
            <div className="mt-2"><ServicesTable services={past} empty="None." /></div>
          </details>
        )}
      </div>

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

      {dialog === 'edit' && <ContractForm contract={k} onClose={() => setDialog(null)} onSaved={() => { setDialog(null); load(); }} />}
      {dialog === 'renew' && <RenewForm contract={k} onClose={() => setDialog(null)} onSaved={() => { setDialog(null); load(); }} />}
      {dialog === 'terminate' && <TerminateForm contract={k} current={current.filter(s => s.charge_type !== 'one_time').length} onClose={() => setDialog(null)} onSaved={() => { setDialog(null); load(); }} />}
      {dialog === 'link' && <LinkServicesForm contract={k} onClose={() => setDialog(null)} onSaved={() => { setDialog(null); load(); }} />}
      {dialog === 'add' && <ServiceForm client={k.client} contract={k} onClose={() => setDialog(null)} onSaved={() => { setDialog(null); load(); }} />}
    </div>
  );
};

// ---- the contracts table ----
const ContractsTable = ({ contracts, showClient, empty }) => (
  <Table head={['Contract', ...(showClient ? ['Client'] : []), 'Term', 'SLA', { label: 'MRC', right: true }, 'State']}>
    {contracts.length === 0 && <tr><td colSpan={showClient ? 6 : 5} className="px-3 py-6 text-center text-zinc-500">{empty}</td></tr>}
    {contracts.map(k => (
      <tr key={k.id} onClick={() => navigate(`contracts/${k.id}`)}
        className="border-t border-zinc-100 dark:border-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer">
        <td className="px-3 py-2 min-w-[12rem]">
          <div className="font-semibold flex items-center gap-1.5">{k.title} {k.doc_name && <Paperclip className="w-3 h-3 text-zinc-400" aria-label="Signed copy attached" />}</div>
          <div className="text-[11px] text-zinc-500"><span className="font-mono">{k.contract_number}</span>{k.client_reference && ` · ${k.client_reference}`}</div>
        </td>
        {showClient && (
          <td className="px-3 py-2 min-w-[10rem]"><div>{k.client?.name}</div><div className="font-mono text-[11px] text-zinc-500">{k.client?.client_number}</div></td>
        )}
        <td className="px-3 py-2 whitespace-nowrap text-xs">
          {termText(k)}
          <div className="text-[11px] text-zinc-500">{k.end_date ? (k.auto_renew ? `auto-renews · notice ${k.notice_days}d` : `notice ${k.notice_days}d`) : ''}</div>
        </td>
        <td className="px-3 py-2 text-xs whitespace-nowrap">{k.sla_plan?.name || '—'}</td>
        <td className="px-3 py-2 text-right whitespace-nowrap">
          <div className="font-semibold">{formatMoney(k.monthly)}</div>
          <div className="text-[11px] text-zinc-500">{k.services} service{k.services === 1 ? '' : 's'}</div>
        </td>
        <td className="px-3 py-2">
          <ContractBadge state={k.state} />
          {k.state === 'due' && <div className="text-[11px] text-amber-600 whitespace-nowrap mt-0.5">{k.auto_renew ? 'renews' : 'ends'} {fmtDay(k.term_end)}</div>}
        </td>
      </tr>
    ))}
  </Table>
);

// ---- on the client's page ----
export const ClientContracts = ({ client }) => {
  const { can } = useAuth();
  const canManage = can('manage_billing');
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);
  useEffect(() => {
    api(`/api/contracts${qs({ client_id: client.id, state: 'all', page_size: 200 })}`).then(setData).catch(err => setError(err.message));
  }, [client.id]);
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
        <h3 className="text-sm font-semibold flex items-center gap-1.5"><Handshake className="w-4 h-4 text-indigo-500" /> Contracts</h3>
        {canManage && client.status !== 'archived' && <Button icon={Plus} onClick={() => setAdding(true)}>New contract</Button>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data ? (!error && <Spinner />) : (
        <ContractsTable contracts={data.contracts} empty={canManage ? 'No contracts yet. Use "New contract".' : 'No contracts.'} />
      )}
      {adding && <ContractForm client={client} onClose={() => setAdding(false)} onSaved={(k) => { setAdding(false); navigate(`contracts/${k.id}`); }} />}
    </div>
  );
};

// ---- all contracts ----
export const ContractsPage = ({ arg, query }) => {
  const { can } = useAuth();
  const canManage = can('manage_billing');
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  const [state, setState] = useState(query?.get('state') || 'current');
  const [page, setPage] = useState(1);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);

  useEffect(() => { setState(query?.get('state') || 'current'); }, [query]);
  useEffect(() => { const t = setTimeout(() => setDebounced(q.trim()), 300); return () => clearTimeout(t); }, [q]);
  useEffect(() => { setPage(1); }, [debounced, state]);
  useEffect(() => {
    const ctrl = new AbortController();
    setError('');
    api(`/api/contracts${qs({ q: debounced, state, page })}`, { signal: ctrl.signal })
      .then(setData).catch(err => { if (err.name !== 'AbortError') setError(err.message); });
    return () => ctrl.abort();
  }, [debounced, state, page]);

  const openId = /^\d+$/.test(arg || '') ? arg : null;
  if (openId) {
    return (
      <>
        <button type="button" onClick={() => window.history.length > 1 ? window.history.back() : navigate('contracts')} className="text-sm text-indigo-600 dark:text-indigo-400 hover:underline mb-4 cursor-pointer">← Back</button>
        <ContractDetail key={openId} id={openId} />
      </>
    );
  }

  const counts = data?.counts || {};
  const dueCount = (counts.due || 0) + (counts.expired || 0);
  return (
    <>
      <PageHeader icon={Handshake} title="Contracts" subtitle="Agreements with clients: term, renewal, notice, service level and the services under each."
        actions={canManage && <Button icon={Plus} onClick={() => setAdding(true)}>New contract</Button>} />
      {dueCount > 0 && state !== 'due' && (
        <Card className="p-3 mb-3 text-sm border-amber-300 dark:border-amber-800 bg-amber-50/60 dark:bg-amber-950/20 flex flex-wrap items-center justify-between gap-2">
          <span className="flex items-center gap-2"><AlertTriangle className="w-4 h-4 text-amber-600" />
            <span><b>{counts.due || 0}</b> contract{counts.due === 1 ? '' : 's'} past the notice date{counts.expired ? <>, <b>{counts.expired}</b> expired without renewal</> : ''}.</span></span>
          <Button variant="secondary" onClick={() => setState('due')}>Show</Button>
        </Card>
      )}
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="relative flex-1 min-w-[14rem] max-w-md">
          <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Client, title, CON no., client's reference…" className={`${inputCls} pl-8`} />
        </span>
        <Select value={state} onChange={(e) => setState(e.target.value)} className="!w-auto">
          <option value="current">Current</option>
          <option value="due">Needs a decision (notice due / expired)</option>
          <option value="draft">Drafts</option>
          <option value="expired">Expired</option>
          <option value="terminated">Terminated</option>
          <option value="all">All</option>
        </Select>
        {data && <span className="text-sm text-zinc-500">{data.total} contract{data.total === 1 ? '' : 's'}</span>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data && !error ? <Spinner /> : data && (
        <>
          <ContractsTable showClient contracts={data.contracts}
            empty={debounced || state !== 'current' ? 'No contracts match.' : canManage ? 'No contracts yet. Add one here or from a client\'s page.' : 'No contracts yet.'} />
          <Pager page={data.page} pageSize={data.page_size} total={data.total} onPage={setPage} />
        </>
      )}
      {adding && <ContractForm onClose={() => setAdding(false)} onSaved={(k) => { setAdding(false); navigate(`contracts/${k.id}`); }} />}
    </>
  );
};
