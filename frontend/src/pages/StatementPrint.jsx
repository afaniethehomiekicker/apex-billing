import React, { useEffect, useState } from 'react';
import { api, apiDownload, qs } from '../lib/api';
import { fmtDay, todayISO, taxIds } from '../lib/format';
import { Spinner, ErrorText } from '../components/ui';

// Client statement (#/statement/12?from=…&to=…): A4, always light, no app
// around it. Print, or "Save as PDF" in the print window; or download Excel.

const money = (v) => {
  const n = Number(v || 0);
  const s = Math.abs(n).toLocaleString('en-PK', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  return n < 0 ? `(${s})` : s;
};
const balText = (v) => (Number(v) < 0 ? `${money(v)} in credit` : money(v));

const defaultFrom = () => {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - 2);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`;
};

const AGEING = ['Opening balance', 'Not yet due', '1–30 days', '31–60 days', '61–90 days', 'Over 90 days', 'Unapplied credit'];

export const StatementPrint = ({ id, query }) => {
  const [from, setFrom] = useState(query?.get('from') || defaultFrom());
  const [to, setTo] = useState(query?.get('to') || todayISO());
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const ctrl = new AbortController();
    const t = setTimeout(() => {
      setError('');
      api(`/api/clients/${id}/statement${qs({ from, to })}`, { signal: ctrl.signal })
        .then(setData).catch(err => { if (err.name !== 'AbortError') setError(err.message); });
    }, 250);
    return () => { clearTimeout(t); ctrl.abort(); };
  }, [id, from, to]);
  useEffect(() => {
    if (!data) return undefined;
    const old = document.title;
    const c = data.statement.client;
    document.title = `Statement ${c.client_number} ${c.name} ${data.statement.from} to ${data.statement.to}`.replace(/[\\/:*?"<>|]/g, '');
    return () => { document.title = old; };
  }, [data]);

  const excel = async () => {
    setBusy(true);
    try { await apiDownload(`/api/clients/${id}/statement${qs({ from, to, format: 'xlsx' })}`, 'statement.xlsx'); } catch (err) { setError(err.message); } finally { setBusy(false); }
  };

  const st = data?.statement;
  const co = st?.company || {};
  const c = st?.client || {};
  const cur = co.currency || 'PKR';

  return (
    <div className="invoice-print min-h-full bg-zinc-200 py-6 print:p-0 print:bg-white">
      <style>{`
        @page { size: A4; margin: 12mm; }
        @media print {
          html, body, #root { height: auto !important; background: #fff !important; }
          .no-print { display: none !important; }
          .sheet { box-shadow: none !important; margin: 0 !important; width: auto !important; min-height: 0 !important; padding: 0 !important; }
          thead { display: table-header-group; }
        }
        .invoice-print, .invoice-print * { color-scheme: light; }
      `}</style>
      <div className="no-print max-w-[210mm] mx-auto mb-3 px-2 flex flex-wrap items-end justify-between gap-3 text-zinc-800">
        <button type="button" onClick={() => { window.location.hash = `#/clients/${id}`; }} className="text-sm text-indigo-700 hover:underline cursor-pointer">← Back to the client</button>
        <div className="flex flex-wrap items-end gap-2">
          <label className="text-xs font-semibold">From<input type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} className="block mt-0.5 px-2 py-1.5 rounded-lg border border-zinc-300 bg-white text-sm font-normal" /></label>
          <label className="text-xs font-semibold">To<input type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} className="block mt-0.5 px-2 py-1.5 rounded-lg border border-zinc-300 bg-white text-sm font-normal" /></label>
          <button type="button" onClick={excel} disabled={!st || busy} className="px-3 py-2 rounded-lg border border-zinc-400 bg-white hover:bg-zinc-50 text-sm font-semibold cursor-pointer disabled:opacity-50">Download Excel</button>
          <button type="button" onClick={() => window.print()} disabled={!st} className="px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-700 text-white text-sm font-semibold cursor-pointer disabled:opacity-50">Print / Save as PDF</button>
        </div>
      </div>
      {error && <div className="no-print max-w-[210mm] mx-auto px-2 mb-2"><ErrorText>{error}</ErrorText></div>}

      <div className="sheet relative max-w-[210mm] min-h-[297mm] mx-auto bg-white text-zinc-900 shadow-lg p-[14mm] text-[12px] leading-snug">
        {!st ? <Spinner /> : (
          <>
            <div className="flex justify-between gap-6 pb-4 border-b-2 border-zinc-900">
              <div className="min-w-0">
                <div className="text-xl font-bold">{co.company_name || 'Your company name'}</div>
                <div className="whitespace-pre-line text-zinc-700">{co.address}</div>
                <div className="text-zinc-700">{[co.phone, co.email, co.website].filter(Boolean).join(' · ')}</div>
                <div className="text-zinc-700">{[co.ntn && `NTN: ${co.ntn}`, co.strn && `STRN: ${co.strn}`].filter(Boolean).join('   ')}</div>
              </div>
              <div className="text-right shrink-0">
                <div className="text-2xl font-black tracking-wide">STATEMENT</div>
                <div className="text-zinc-600">of account</div>
                <table className="ml-auto mt-2 text-left">
                  <tbody>
                    <tr><td className="pr-3 text-zinc-500">Period</td><td>{fmtDay(st.from)} – {fmtDay(st.to)}</td></tr>
                    <tr><td className="pr-3 text-zinc-500">Customer no.</td><td className="font-mono">{c.client_number}</td></tr>
                    <tr><td className="pr-3 text-zinc-500">Balance due</td><td className="font-bold">{cur} {balText(st.closing)}</td></tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div className="mt-4 mb-4">
              <div className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Statement for</div>
              <div className="text-[14px] font-bold">{c.name}</div>
              {c.contact_person && c.client_type !== 'individual' && <div>Attn: {c.contact_person}</div>}
              <div className="whitespace-pre-line">{[c.address, [c.city, c.province].filter(Boolean).join(', ')].filter(Boolean).join('\n')}</div>
              <div className="text-zinc-700">{taxIds({ type: c.client_type, ntn: c.ntn, strn: c.strn, cnic: c.cnic })}</div>
            </div>

            <table className="w-full border-collapse">
              <thead>
                <tr className="bg-zinc-900 text-white text-[11px]">
                  <th className="py-1.5 px-2 text-left w-24">Date</th>
                  <th className="py-1.5 px-2 text-left w-24">Reference</th>
                  <th className="py-1.5 px-2 text-left">Details</th>
                  <th className="py-1.5 px-2 text-right w-24">Invoiced</th>
                  <th className="py-1.5 px-2 text-right w-24">Paid</th>
                  <th className="py-1.5 px-2 text-right w-28">Balance ({cur})</th>
                </tr>
              </thead>
              <tbody>
                <tr className="border-b border-zinc-200 bg-zinc-50">
                  <td className="py-1.5 px-2">{fmtDay(st.from)}</td><td className="py-1.5 px-2" />
                  <td className="py-1.5 px-2 font-semibold">Balance brought forward</td><td /><td />
                  <td className="py-1.5 px-2 text-right font-semibold">{money(st.brought_forward)}</td>
                </tr>
                {st.lines.map((l, i) => (
                  <tr key={i} className="border-b border-zinc-200 align-top" style={{ breakInside: 'avoid' }}>
                    <td className="py-1.5 px-2 whitespace-nowrap">{l.date ? fmtDay(l.date) : '—'}</td>
                    <td className="py-1.5 px-2 font-mono text-[11px]">
                      {l.link ? <a href={`#/${l.link}`} className="text-zinc-900 no-underline hover:underline">{l.ref}</a> : l.ref}
                    </td>
                    <td className="py-1.5 px-2">{l.details}</td>
                    <td className="py-1.5 px-2 text-right">{l.debit ? money(l.debit) : ''}</td>
                    <td className="py-1.5 px-2 text-right">{l.credit ? money(l.credit) : ''}</td>
                    <td className="py-1.5 px-2 text-right">{money(l.balance)}</td>
                  </tr>
                ))}
                {st.lines.length === 0 && (
                  <tr><td colSpan={6} className="py-3 px-2 text-center text-zinc-500">No invoices or payments in this period.</td></tr>
                )}
              </tbody>
              <tfoot>
                <tr className="border-t-2 border-zinc-900 font-bold" style={{ breakInside: 'avoid' }}>
                  <td className="py-1.5 px-2">{fmtDay(st.to)}</td><td />
                  <td className="py-1.5 px-2">Closing balance</td>
                  <td className="py-1.5 px-2 text-right">{money(st.total_debit)}</td>
                  <td className="py-1.5 px-2 text-right">{money(st.total_credit)}</td>
                  <td className="py-1.5 px-2 text-right text-[13px]">{money(st.closing)}</td>
                </tr>
              </tfoot>
            </table>
            <div className="mt-1 text-[10px] text-zinc-500">Amounts in brackets are in your favour (credit).</div>

            <div className="mt-5" style={{ breakInside: 'avoid' }}>
              <div className="text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">Balance by age, as of {fmtDay(st.to)}</div>
              <table className="w-full border border-zinc-300 text-center">
                <thead><tr className="bg-zinc-100 text-[10px]">{AGEING.map(a => <th key={a} className="py-1 px-1 font-semibold border-r border-zinc-300 last:border-r-0">{a}</th>)}<th className="py-1 px-1 font-bold">Balance</th></tr></thead>
                <tbody><tr>{st.ageing.map((v, i) => <td key={i} className="py-1.5 px-1 border-r border-zinc-300">{v ? money(v) : '–'}</td>)}<td className="py-1.5 px-1 font-bold">{money(st.closing)}</td></tr></tbody>
              </table>
            </div>

            {st.pending > 0 && (
              <div className="mt-3 text-[11px] text-zinc-700">Payments of {cur} {money(st.pending)} you have sent are being checked and are not included above yet.</div>
            )}

            {co.bank_details && st.closing > 0 && (
              <div className="mt-5 p-3 border border-zinc-300 rounded" style={{ breakInside: 'avoid' }}>
                <div className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Please pay to</div>
                <div className="whitespace-pre-line">{co.bank_details}</div>
                <div className="mt-1 text-[11px] text-zinc-600">Please quote your customer no. <b>{c.client_number}</b> and the invoice numbers with your payment.</div>
              </div>
            )}

            <div className="mt-5 text-[11px] text-zinc-600">If anything on this statement doesn't match your records, please contact us{co.email ? ` at ${co.email}` : ''}{co.phone ? ` or ${co.phone}` : ''}.</div>
            {co.invoice_footer && <div className="mt-6 pt-2 border-t border-zinc-300 text-center text-[11px] text-zinc-600 whitespace-pre-line">{co.invoice_footer}</div>}
            <div className="mt-4 text-center text-[10px] text-zinc-400">This is a computer-generated statement.</div>
          </>
        )}
      </div>
    </div>
  );
};
