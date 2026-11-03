import React, { useEffect, useState } from 'react';
import { ShieldCheck, Plus, Pencil, Trash2, History } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth, navigate } from '../lib/auth';
import { fmtDateTime, SUPPORT_HOURS, minutesText, pctText } from '../lib/format';
import {
  PageHeader, Button, Field, Input, Select, Textarea, Badge, Modal, Table, Spinner, ErrorText, Card,
} from '../components/ui';

// Service levels (SLAs): uptime, response and restore targets, support
// hours and the credits owed when a month falls short. Each service has
// one; a contract can give a default for the services under it.

const EMPTY = {
  name: '', code: '', uptime_target: '99.5', response_h: '', response_m: '', restore_h: '', restore_m: '',
  support_hours: '24x7', support_note: '', credit_tiers: [], max_credit_percent: '', description: '', active: true,
};

const splitMin = (m) => {
  m = Number(m) || 0;
  return m ? [String(Math.floor(m / 60) || ''), String(m % 60 || '')] : ['', ''];
};
const toMin = (h, m) => Math.round((Number(h) || 0) * 60 + (Number(m) || 0));
const num = (v) => Number(String(v ?? '').replace(/,/g, '').trim() || '0');

const fromPlan = (p) => {
  const [rh, rm] = splitMin(p.response_minutes);
  const [sh, sm] = splitMin(p.restore_minutes);
  return {
    ...EMPTY, ...p, uptime_target: String(Number(p.uptime_target)), response_h: rh, response_m: rm, restore_h: sh, restore_m: sm,
    max_credit_percent: Number(p.max_credit_percent) ? String(Number(p.max_credit_percent)) : '',
    credit_tiers: (p.credit_tiers || []).map(t => ({ below_percent: String(t.below_percent), credit_percent: String(t.credit_percent) })),
  };
};

export const SlaBadge = ({ plan }) => (plan ? <Badge tone="indigo">{plan.name}</Badge> : null);

// Credit steps as a sentence list: "Below 99.5%: 5% of the monthly charge".
export const CreditSteps = ({ plan }) => {
  const tiers = [...(plan.credit_tiers || [])].sort((a, b) => b.below_percent - a.below_percent);
  if (tiers.length === 0) return <span className="text-zinc-500">No service credits.</span>;
  return (
    <ul className="space-y-0.5">
      {tiers.map((t, i) => (
        <li key={i}>Uptime below <b>{pctText(t.below_percent)}</b> → credit <b>{pctText(t.credit_percent)}</b> of the month's charge</li>
      ))}
      {Number(plan.max_credit_percent) > 0 && <li className="text-zinc-500">At most {pctText(plan.max_credit_percent)} in any month.</li>}
    </ul>
  );
};

const SlaForm = ({ plan, onClose, onSaved }) => {
  const [form, setForm] = useState(() => (plan ? fromPlan(plan) : EMPTY));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [history, setHistory] = useState(null);
  const set = (k) => (e) => setForm(f => ({ ...f, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));
  const setTier = (i, k) => (e) => setForm(f => ({ ...f, credit_tiers: f.credit_tiers.map((t, j) => (j === i ? { ...t, [k]: e.target.value } : t)) }));

  useEffect(() => {
    if (plan) api(`/api/sla-plans/${plan.id}`).then(setHistory).catch(() => {});
  }, [plan]);

  const addTier = () => setForm(f => {
    const last = f.credit_tiers[f.credit_tiers.length - 1];
    const below = last ? String(Math.max(0, num(last.below_percent) - 0.5)) : (f.uptime_target || '99.5');
    const credit = last ? String(Math.min(100, num(last.credit_percent) + 5)) : '5';
    return { ...f, credit_tiers: [...f.credit_tiers, { below_percent: below, credit_percent: credit }] };
  });

  const save = async () => {
    setError('');
    if (!form.name.trim()) { setError('Enter the name of the service level'); return; }
    const body = {
      name: form.name, code: form.code, uptime_target: num(form.uptime_target),
      response_minutes: toMin(form.response_h, form.response_m), restore_minutes: toMin(form.restore_h, form.restore_m),
      support_hours: form.support_hours, support_note: form.support_note,
      credit_tiers: form.credit_tiers.map(t => ({ below_percent: num(t.below_percent), credit_percent: num(t.credit_percent) })),
      max_credit_percent: num(form.max_credit_percent), description: form.description, active: !!form.active,
    };
    if ([body.uptime_target, body.max_credit_percent, ...body.credit_tiers.flatMap(t => [t.below_percent, t.credit_percent])].some(v => !Number.isFinite(v))) {
      setError('Check the numbers'); return;
    }
    setBusy(true);
    try {
      const d = plan
        ? await api(`/api/sla-plans/${plan.id}`, { method: 'PUT', body })
        : await api('/api/sla-plans', { method: 'POST', body });
      onSaved(d.plan);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const hm = (h, m) => (
    <div className="flex items-center gap-1.5">
      <Input inputMode="numeric" value={form[h]} onChange={set(h)} maxLength={4} placeholder="0" className="!w-16" /> <span className="text-xs text-zinc-500">h</span>
      <Input inputMode="numeric" value={form[m]} onChange={set(m)} maxLength={2} placeholder="0" className="!w-16" /> <span className="text-xs text-zinc-500">min</span>
    </div>
  );

  return (
    <Modal wide title={plan ? `Edit ${plan.name}` : 'Add service level'} onClose={onClose}
      footer={<>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button busy={busy} onClick={save}>{plan ? 'Save changes' : 'Add service level'}</Button>
      </>}>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <Field label="Name" required className="sm:col-span-2" hint="e.g. Gold, Standard, Enterprise 99.9%.">
          <Input autoFocus value={form.name} onChange={set('name')} maxLength={100} />
        </Field>
        <Field label="Code" hint="Optional.">
          <Input value={form.code} onChange={set('code')} maxLength={30} placeholder="SLA-GOLD" />
        </Field>
        <Field label="Uptime target (% a month)" hint="0 = no uptime commitment.">
          <Input inputMode="decimal" value={form.uptime_target} onChange={set('uptime_target')} maxLength={7} />
        </Field>
        <Field label="Response time" hint="First response to a reported fault.">{hm('response_h', 'response_m')}</Field>
        <Field label="Restore time" hint="Service back up (MTTR target).">{hm('restore_h', 'restore_m')}</Field>
        <Field label="Support hours">
          <Select value={form.support_hours} onChange={set('support_hours')}>
            {Object.entries(SUPPORT_HOURS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
          </Select>
        </Field>
        <Field label="Support hours note" className="sm:col-span-2" hint={form.support_hours === 'custom' ? 'Required for "Other".' : 'Optional, e.g. Mon–Sat 9:00–18:00.'}>
          <Input value={form.support_note} onChange={set('support_note')} maxLength={200} />
        </Field>

        <div className="sm:col-span-3">
          <div className="text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1">Service credits</div>
          <p className="text-[11px] text-zinc-500 mb-2">When a month's uptime falls below a step, the client is owed that % of the service's monthly charge. Lower uptime → same or bigger credit.</p>
          <div className="space-y-2">
            {form.credit_tiers.map((t, i) => (
              <div key={i} className="flex flex-wrap items-center gap-2 text-sm">
                <span className="text-zinc-500">Uptime below</span>
                <Input inputMode="decimal" value={t.below_percent} onChange={setTier(i, 'below_percent')} maxLength={7} className="!w-24" />
                <span className="text-zinc-500">% → credit</span>
                <Input inputMode="decimal" value={t.credit_percent} onChange={setTier(i, 'credit_percent')} maxLength={6} className="!w-20" />
                <span className="text-zinc-500">%</span>
                <button type="button" onClick={() => setForm(f => ({ ...f, credit_tiers: f.credit_tiers.filter((_, j) => j !== i) }))}
                  className="p-1.5 rounded text-zinc-500 hover:text-rose-600 hover:bg-zinc-100 dark:hover:bg-zinc-800 cursor-pointer" aria-label="Remove step"><Trash2 className="w-4 h-4" /></button>
              </div>
            ))}
            <div className="flex flex-wrap items-center gap-3">
              <Button variant="secondary" icon={Plus} onClick={addTier} disabled={form.credit_tiers.length >= 10}>Add credit step</Button>
              {form.credit_tiers.length > 0 && (
                <label className="flex items-center gap-2 text-sm">
                  <span className="text-zinc-500">Cap per month</span>
                  <Input inputMode="decimal" value={form.max_credit_percent} onChange={set('max_credit_percent')} maxLength={6} placeholder="none" className="!w-20" />
                  <span className="text-zinc-500">%</span>
                </label>
              )}
            </div>
          </div>
        </div>

        <Field label="Terms and exclusions" className="sm:col-span-3" hint="e.g. planned maintenance with 48 hours' notice is not downtime; faults on the client's side are excluded.">
          <Textarea rows={3} value={form.description} onChange={set('description')} maxLength={5000} />
        </Field>
        {plan && (
          <label className="sm:col-span-3 flex items-center gap-2 text-sm">
            <input type="checkbox" checked={!!form.active} onChange={set('active')} /> Offered (inactive levels can't be given to new services; existing ones keep it)
          </label>
        )}
      </div>
      <div className="mt-3"><ErrorText>{error}</ErrorText></div>
      {history && (
        <div className="mt-4 pt-3 border-t border-zinc-200 dark:border-zinc-800">
          <p className="text-xs text-zinc-500 mb-2">Used by {history.services} current service{history.services === 1 ? '' : 's'} and {history.contracts} contract{history.contracts === 1 ? '' : 's'} as default.</p>
          <h3 className="text-xs font-semibold mb-1.5 flex items-center gap-1.5"><History className="w-3.5 h-3.5 text-indigo-500" /> History</h3>
          {history.history.length === 0 ? <p className="text-xs text-zinc-500">No changes recorded.</p> : (
            <ol className="space-y-1 max-h-40 overflow-y-auto">
              {history.history.map(h => (
                <li key={h.id} className="text-xs text-zinc-700 dark:text-zinc-300"><span className="text-zinc-500">{fmtDateTime(h.created_at)}</span> · <b>{h.user_name}</b> · {h.action} · {h.details}</li>
              ))}
            </ol>
          )}
        </div>
      )}
    </Modal>
  );
};

export const SlaPage = () => {
  const { can } = useAuth();
  const canManage = can('manage_billing');
  const [status, setStatus] = useState('active');
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [form, setForm] = useState(null); // null | { plan? }
  const [reload, setReload] = useState(0);

  useEffect(() => {
    setError('');
    api(`/api/sla-plans?status=${status}`).then(setData).catch(err => setError(err.message));
  }, [status, reload]);

  return (
    <>
      <PageHeader icon={ShieldCheck} title="Service levels (SLAs)" subtitle="Uptime, response and restore targets, and the credits owed when a month falls short."
        actions={canManage && <Button icon={Plus} onClick={() => setForm({})}>Add service level</Button>} />
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <Select value={status} onChange={(e) => setStatus(e.target.value)} className="!w-auto">
          <option value="active">Offered</option>
          <option value="inactive">Inactive</option>
          <option value="all">All</option>
        </Select>
        {data && <span className="text-sm text-zinc-500">{data.plans.length} service level{data.plans.length === 1 ? '' : 's'}</span>}
      </div>
      <ErrorText>{error}</ErrorText>
      {!data ? (!error && <Spinner />) : data.plans.length === 0 ? (
        <Card className="p-6 text-sm text-zinc-500 text-center">
          {status === 'active' ? (canManage ? 'No service levels yet. Add your SLAs (e.g. Standard 99%, Gold 99.5%, Platinum 99.9%), then choose one on each service.' : 'No service levels yet.') : 'None.'}
        </Card>
      ) : (
        <Table head={['Service level', 'Uptime', 'Response', 'Restore', 'Support', 'Credits', { label: 'Services', right: true }, '']}>
          {data.plans.map(p => (
            <tr key={p.id} className="border-t border-zinc-100 dark:border-zinc-800 align-top">
              <td className="px-3 py-2 min-w-[10rem]">
                <div className="font-semibold flex items-center gap-2">{p.name} {!p.active && <Badge>Inactive</Badge>}</div>
                {p.code && <div className="font-mono text-[11px] text-zinc-500">{p.code}</div>}
              </td>
              <td className="px-3 py-2 whitespace-nowrap font-semibold">{Number(p.uptime_target) > 0 ? pctText(p.uptime_target) : '—'}</td>
              <td className="px-3 py-2 whitespace-nowrap">{minutesText(p.response_minutes)}</td>
              <td className="px-3 py-2 whitespace-nowrap">{minutesText(p.restore_minutes)}</td>
              <td className="px-3 py-2 min-w-[8rem]">{SUPPORT_HOURS[p.support_hours]}{p.support_note && <div className="text-[11px] text-zinc-500">{p.support_note}</div>}</td>
              <td className="px-3 py-2 text-xs min-w-[14rem]"><CreditSteps plan={p} /></td>
              <td className="px-3 py-2 text-right">
                {data.in_use[p.id]
                  ? <button type="button" onClick={() => navigate(`services?sla=${p.id}`)} className="text-indigo-600 dark:text-indigo-400 hover:underline cursor-pointer">{data.in_use[p.id]}</button>
                  : <span className="text-zinc-400">0</span>}
              </td>
              <td className="px-3 py-2 text-right">
                {canManage && <Button variant="ghost" icon={Pencil} onClick={() => setForm({ plan: p })} aria-label={`Edit ${p.name}`} />}
              </td>
            </tr>
          ))}
        </Table>
      )}
      {data && data.plans.length > 0 && <p className="text-[11px] text-zinc-500 mt-2">A credit owed is worked out on the service's page ("SLA credit check") and given on the next invoice as a minus line.</p>}
      {form && (
        <SlaForm plan={form.plan} onClose={() => setForm(null)} onSaved={() => { setForm(null); setReload(r => r + 1); }} />
      )}
    </>
  );
};
