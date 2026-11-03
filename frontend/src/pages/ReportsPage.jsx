import React, { useEffect, useMemo, useState } from 'react';
import { BarChart3, Download } from 'lucide-react';
import { api, apiDownload, qs } from '../lib/api';
import { navigate } from '../lib/auth';
import { formatMoney, fmtDay, todayISO, monthISO } from '../lib/format';
import { PageHeader, Button, Field, Input, Select, Spinner, ErrorText, Card } from '../components/ui';
import { CompanySelect } from '../components/CompanySelect';
import { useCompanies } from '../lib/companies';

// Reports: ageing, billed vs collected, MRC, invoice and payment registers.
// Every report downloads as Excel with the same columns. #/reports/ageing etc.

const REPORTS = [
  { key: 'ageing', label: 'Ageing', hint: 'What each client owes, by how late it is.' },
  { key: 'billed-collected', label: 'Billed vs collected', hint: 'Month by month: invoiced and money received.' },
  { key: 'mrc', label: 'MRC', hint: 'Monthly recurring revenue being billed now.' },
  { key: 'invoices', label: 'Invoice register', hint: 'Every invoice in a period (sales tax returns).' },
  { key: 'payments', label: 'Payment register', hint: 'Every payment in a period, with tax deducted.' },
  { key: 'taxes', label: 'Tax summary', hint: 'Each tax on the invoices issued in a period: taxable amount and tax (for tax returns).' },
];

const monthsAgo = (n) => {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
};
const firstOfMonth = () => `${monthISO()}-01`;
const endOfMonth = () => {
  const d = new Date();
  const e = new Date(d.getFullYear(), d.getMonth() + 1, 0);
  return `${e.getFullYear()}-${String(e.getMonth() + 1).padStart(2, '0')}-${String(e.getDate()).padStart(2, '0')}`;
};

const DEFAULTS = {
  ageing: () => ({ as_of: todayISO() }),
  'billed-collected': () => ({ from: monthsAgo(11), to: monthISO() }),
  mrc: () => ({ group: 'product' }),
  invoices: () => ({ from: firstOfMonth(), to: endOfMonth() }),
  payments: () => ({ from: firstOfMonth(), to: endOfMonth(), status: 'verified' }),
  taxes: () => ({ from: firstOfMonth(), to: endOfMonth() }),
};

const show = (v, kind) => {
  if (v === null || v === undefined || v === '') return '';
  if (kind === 'money') return formatMoney(v).replace(/^PKR /, '').replace(/^−PKR /, '−');
  if (kind === 'date') return fmtDay(v);
  if (kind === 'percent') return `${Number(v).toFixed(2)}%`;
  return String(v);
};
const isNum = (k) => k === 'money' || k === 'number' || k === 'percent';

// ---- billed vs collected: grouped bars, one axis, hover tooltip ----
const compact = (v) => {
  const a = Math.abs(v);
  if (a >= 1e7) return `${(v / 1e7).toFixed(a >= 1e8 ? 0 : 1)} cr`;
  if (a >= 1e5) return `${(v / 1e5).toFixed(a >= 1e6 ? 0 : 1)} lac`;
  if (a >= 1e3) return `${Math.round(v / 1e3)}k`;
  return String(Math.round(v));
};
const niceMax = (v) => {
  if (v <= 0) return 1;
  const p = 10 ** Math.floor(Math.log10(v));
  return [1, 2, 2.5, 5, 10].map(m => m * p).find(x => x >= v);
};
const monthLabel = (k) => {
  const [y, m] = k.split('-').map(Number);
  return new Date(y, m - 1, 1).toLocaleDateString('en-GB', { month: 'short' }) + (m === 1 ? ` ${String(y).slice(2)}` : '');
};

const BilledCollectedChart = ({ points }) => {
  const [hover, setHover] = useState(null);
  const W = 760; const H = 240; const L = 56; const R = 8; const T = 12; const B = 28;
  const max = niceMax(Math.max(...points.map(p => Math.max(p.billed, p.collected)), 0));
  const ticks = [0, 0.25, 0.5, 0.75, 1].map(f => f * max);
  const band = (W - L - R) / points.length;
  const bw = Math.max(3, Math.min(18, (band - 10) / 2));
  const y = (v) => T + (H - T - B) * (1 - v / max);
  const bar = (x, v, color) => {
    const top = y(v); const h = H - B - top;
    if (h <= 0) return null;
    const r = Math.min(4, h, bw / 2);
    // rounded data end (top), square at the baseline
    return <path d={`M${x},${H - B} V${top + r} Q${x},${top} ${x + r},${top} H${x + bw - r} Q${x + bw},${top} ${x + bw},${top + r} V${H - B} Z`} fill={color} />;
  };
  const hp = hover !== null ? points[hover] : null;
  return (
    <Card className="p-4 mb-4">
      <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
        <div className="text-sm font-semibold">Billed vs collected, by month (PKR, with tax)</div>
        <div className="flex gap-4 text-xs text-zinc-600 dark:text-zinc-300">
          <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded-sm" style={{ background: 'var(--series-1)' }} /> Billed</span>
          <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded-sm" style={{ background: 'var(--series-2)' }} /> Collected</span>
        </div>
      </div>
      <div className="relative">
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full h-auto" role="img" aria-label="Billed and collected per month; the table below has the figures">
          {ticks.map((t, i) => (
            <g key={i}>
              <line x1={L} x2={W - R} y1={y(t)} y2={y(t)} stroke="var(--chart-grid)" strokeWidth="1" />
              <text x={L - 6} y={y(t) + 3.5} textAnchor="end" className="fill-zinc-500" fontSize="10">{compact(t)}</text>
            </g>
          ))}
          {points.map((p, i) => {
            const x0 = L + i * band + (band - (bw * 2 + 2)) / 2;
            return (
              <g key={p.month}>
                {hover === i && <rect x={L + i * band} y={T} width={band} height={H - T - B} className="fill-zinc-500/10" />}
                {bar(x0, p.billed, 'var(--series-1)')}
                {bar(x0 + bw + 2, p.collected, 'var(--series-2)')}
                <text x={L + i * band + band / 2} y={H - 10} textAnchor="middle" className="fill-zinc-500" fontSize="10">{monthLabel(p.month)}</text>
                <rect x={L + i * band} y={T} width={band} height={H - T - B + 20} fill="transparent"
                  onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)} onClick={() => setHover(i)} />
              </g>
            );
          })}
        </svg>
        {hp && (
          <div className="pointer-events-none absolute top-0 z-10 rounded-lg border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 shadow-lg px-3 py-2 text-xs"
            style={{ left: `${Math.min(70, Math.max(0, ((L + hover * band) / W) * 100))}%` }}>
            <div className="font-semibold mb-1">{new Date(`${hp.month}-01T00:00`).toLocaleDateString('en-GB', { month: 'long', year: 'numeric' })}</div>
            <div className="flex items-center gap-1.5"><span className="w-2 h-2 rounded-sm" style={{ background: 'var(--series-1)' }} /> Billed <b className="ml-auto pl-3">{formatMoney(hp.billed)}</b></div>
            <div className="flex items-center gap-1.5"><span className="w-2 h-2 rounded-sm" style={{ background: 'var(--series-2)' }} /> Collected <b className="ml-auto pl-3">{formatMoney(hp.collected)}</b></div>
          </div>
        )}
      </div>
    </Card>
  );
};

// ---- one report ----
const ReportTable = ({ report }) => (
  <div className="overflow-x-auto rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900">
    <table className="w-full text-sm">
      <thead className="bg-zinc-50 dark:bg-zinc-800/60 text-zinc-600 dark:text-zinc-300 text-xs">
        <tr>{report.columns.map(c => <th key={c.key} className={`px-3 py-2 font-semibold whitespace-nowrap ${isNum(c.kind) ? 'text-right' : 'text-left'}`}>{c.label}</th>)}</tr>
      </thead>
      <tbody>
        {report.rows.length === 0 && <tr><td colSpan={report.columns.length} className="px-3 py-6 text-center text-zinc-500">Nothing in this report.</td></tr>}
        {report.rows.map((row, i) => (
          <tr key={i} onClick={() => report.links?.[i] && navigate(report.links[i])}
            className={`border-t border-zinc-100 dark:border-zinc-800 ${report.links?.[i] ? 'hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer' : ''}`}>
            {report.columns.map((c, j) => (
              <td key={c.key} className={`px-3 py-1.5 whitespace-nowrap ${isNum(c.kind) ? 'text-right tabular-nums' : ''} ${c.kind === 'money' && Number(row[j]) < 0 ? 'text-rose-600' : ''}`}>
                {show(row[j], c.kind)}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
      {report.totals && report.rows.length > 0 && (
        <tfoot>
          <tr className="border-t-2 border-zinc-300 dark:border-zinc-700 font-bold">
            {report.columns.map((c, j) => (
              <td key={c.key} className={`px-3 py-2 whitespace-nowrap ${isNum(c.kind) ? 'text-right tabular-nums' : ''}`}>{show(report.totals[j], c.kind)}</td>
            ))}
          </tr>
        </tfoot>
      )}
    </table>
  </div>
);

// Reports that can be for one billing company.
const BY_COMPANY = ['mrc', 'invoices', 'taxes'];

export const ReportsPage = ({ arg }) => {
  const key = REPORTS.find(r => r.key === arg)?.key || 'ageing';
  const companies = useCompanies();
  const [params, setParams] = useState(() => DEFAULTS[key]());
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => { setParams(DEFAULTS[key]()); setData(null); }, [key]);
  const query = useMemo(() => qs(params), [params]);
  useEffect(() => {
    const ctrl = new AbortController();
    const t = setTimeout(() => {
      setError('');
      api(`/api/reports/${key}${query}`, { signal: ctrl.signal })
        .then(d => setData({ key, report: d.report })).catch(err => { if (err.name !== 'AbortError') setError(err.message); });
    }, 250);
    return () => { clearTimeout(t); ctrl.abort(); };
  }, [key, query]);

  const set = (k) => (e) => setParams(p => ({ ...p, [k]: e.target.value }));
  const download = async () => {
    setBusy(true);
    setError('');
    try {
      await apiDownload(`/api/reports/${key}${qs({ ...params, format: 'xlsx' })}`, `${key}.xlsx`);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  const report = data?.key === key ? data.report : null;
  const info = REPORTS.find(r => r.key === key);

  return (
    <>
      <PageHeader icon={BarChart3} title="Reports" subtitle="Every report downloads as Excel."
        actions={<Button icon={Download} busy={busy} disabled={!report} onClick={download}>Download Excel</Button>} />
      <div className="flex flex-wrap gap-1 mb-4 p-1 rounded-xl bg-zinc-100 dark:bg-zinc-900 w-fit max-w-full">
        {REPORTS.map(r => (
          <a key={r.key} href={`#/reports/${r.key}`}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium whitespace-nowrap ${r.key === key ? 'bg-white dark:bg-zinc-800 shadow-sm text-zinc-900 dark:text-white' : 'text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-white'}`}>
            {r.label}
          </a>
        ))}
      </div>

      <div className="flex flex-wrap items-end gap-3 mb-4">
        {key === 'ageing' && <Field label="As of"><Input type="date" value={params.as_of || ''} onChange={set('as_of')} /></Field>}
        {key === 'billed-collected' && <>
          <Field label="From month"><Input type="month" value={params.from || ''} onChange={set('from')} /></Field>
          <Field label="To month"><Input type="month" value={params.to || ''} onChange={set('to')} /></Field>
        </>}
        {key === 'mrc' && (
          <Field label="Group by">
            <Select value={params.group} onChange={set('group')}><option value="product">Product</option><option value="client">Client</option></Select>
          </Field>
        )}
        {(key === 'invoices' || key === 'payments' || key === 'taxes') && <>
          <Field label="From"><Input type="date" value={params.from || ''} onChange={set('from')} /></Field>
          <Field label="To"><Input type="date" value={params.to || ''} onChange={set('to')} /></Field>
        </>}
        {companies.length > 1 && BY_COMPANY.includes(key) && (
          <Field label="Company">
            <CompanySelect companies={companies} value={params.company_id || ''} allLabel="All companies" onChange={(v) => setParams(p => ({ ...p, company_id: v }))} />
          </Field>
        )}
        {key === 'payments' && (
          <Field label="Status">
            <Select value={params.status} onChange={set('status')}>
              <option value="verified">Verified</option>
              <option value="pending">Waiting for verification</option>
              <option value="rejected">Rejected</option>
              <option value="reversed">Reversed</option>
              <option value="all">All</option>
            </Select>
          </Field>
        )}
        <p className="text-xs text-zinc-500 pb-2">{report?.subtitle || info.hint}</p>
      </div>

      <ErrorText>{error}</ErrorText>
      {!report ? (!error && <Spinner />) : (
        <>
          {key === 'billed-collected' && report.chart?.length > 0 && <BilledCollectedChart points={report.chart} />}
          <ReportTable report={report} />
          {key === 'ageing' && <p className="mt-2 text-xs text-zinc-500">Days late are counted from each invoice's due date. Unapplied credit (verified payments not yet put against an invoice) is shown as a minus and reduces the balance.</p>}
        </>
      )}
    </>
  );
};
