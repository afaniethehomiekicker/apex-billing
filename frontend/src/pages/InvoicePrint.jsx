import React, { useEffect, useState } from 'react';
import { api } from '../lib/api';
import { fmtDay, linePeriod, amountInWords, taxIds, pctText, taxShort } from '../lib/format';
import { Spinner, ErrorText } from '../components/ui';
import { CompanyLogo } from '../components/CompanyLogo';

// The printed invoice (#/print/12): A4, always light, no app around it.
// "Print / Save as PDF" uses the browser's print window, where "Save as
// PDF" makes the PDF.
//
// Layout: company + invoice details, bill to + amount due, lines grouped by
// the product's category (A · IT Services, B · …; numbered A1, A2…), then
// payment details, totals per category, amount in words, terms & notes.

const r2 = (v) => Math.round((Number(v) || 0) * 100) / 100;

// "01 Nov – 30 Nov 2026"
const rangeText = (a, b) => {
  if (!a || !b) return '';
  const da = fmtDay(a);
  const db = fmtDay(b);
  if (!da || !db) return '';
  if (da === db) return da;
  return da.slice(-4) === db.slice(-4) ? `${da.slice(0, -5)} – ${db}` : `${da} – ${db}`;
};
const monthRange = (ym) => {
  const m = /^(\d{4})-(\d{2})/.exec(ym || '');
  if (!m) return '';
  const last = new Date(Number(m[1]), Number(m[2]), 0).getDate();
  return rangeText(`${m[1]}-${m[2]}-01`, `${m[1]}-${m[2]}-${String(last).padStart(2, '0')}`);
};
const dayDiff = (a, b) => {
  const pa = Date.parse(String(a || '').slice(0, 10));
  const pb = Date.parse(String(b || '').slice(0, 10));
  return Number.isFinite(pa) && Number.isFinite(pb) ? Math.round((pb - pa) / 86400000) : null;
};

const Label = ({ children, className = '' }) => (
  <div className={`text-[9.5px] font-semibold uppercase tracking-[0.12em] text-[#2563eb] ${className}`}>{children}</div>
);

export const InvoicePrint = ({ id }) => {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    api(`/api/invoices/${id}`).then(setData).catch(err => setError(err.message));
  }, [id]);
  useEffect(() => {
    if (!data) return undefined;
    const inv = data.invoice;
    const old = document.title;
    // The PDF's file name comes from the page title.
    document.title = `${inv.invoice_number || `Draft-${inv.id}`} ${inv.bill_name}`.replace(/[\\/:*?"<>|]/g, '');
    return () => { document.title = old; };
  }, [data]);

  if (error) return <div className="p-6"><ErrorText>{error}</ErrorText></div>;
  if (!data) return <div className="p-6"><Spinner /></div>;
  const inv = data.invoice;
  const co = data.company || {};
  const cur = inv.currency || co.currency || 'PKR';
  const stamp = inv.status === 'draft' ? 'DRAFT' : inv.status === 'cancelled' ? 'CANCELLED' : '';
  const back = () => { window.location.hash = `#/invoices/${inv.id}`; };
  const lines = inv.lines || [];

  // Whole rupees when nothing on the invoice has paisa; otherwise 2 decimals.
  const hasPaisa = [...lines.flatMap(l => [l.rate, l.amount]), inv.subtotal, inv.total, inv.amount_paid,
    ...(inv.tax_lines || []).map(t => t.amount)].some(v => Math.abs(r2(v) - Math.round(r2(v))) > 0.001);
  const money = (v) => Number(v || 0).toLocaleString('en-PK', { minimumFractionDigits: hasPaisa ? 2 : 0, maximumFractionDigits: 2 });

  // Unit and category come from each service line's product.
  const info = new Map((data.line_info || []).map(x => [x.line_id, x]));
  const catOf = (l) => (l.category || info.get(l.id)?.category || '').trim();
  const unitOf = (l) => l.unit || info.get(l.id)?.unit || '';
  const anyCat = lines.some(l => catOf(l));
  const groups = [];
  if (anyCat) {
    const byName = new Map();
    lines.forEach((l) => {
      const name = catOf(l) || 'Other charges';
      if (!byName.has(name)) { byName.set(name, { name, lines: [] }); groups.push(byName.get(name)); }
      byName.get(name).lines.push(l);
    });
    // Uncategorised lines last.
    groups.sort((a, b) => (a.name === 'Other charges') - (b.name === 'Other charges'));
    groups.forEach((g, i) => {
      g.letter = i < 26 ? String.fromCharCode(65 + i) : String(i + 1);
      g.total = r2(g.lines.reduce((s, l) => s + Number(l.amount || 0), 0));
    });
  } else {
    groups.push({ name: '', letter: '', lines, total: r2(inv.subtotal) });
  }

  const taxLines = inv.tax_lines?.length
    ? inv.tax_lines
    : (Number(inv.tax_amount) ? [{ name: 'Sales tax', rate: Number(inv.tax_percent), kind: 'add', amount: Number(inv.tax_amount) }] : []);
  const discount = r2(lines.reduce((s, l) => s + Math.min(0, Number(l.amount || 0)), 0));
  const gross = r2(Number(inv.subtotal) - discount);
  const paid = inv.status === 'issued' ? Number(inv.amount_paid) || 0 : 0;
  const balance = Math.max(0, r2(Number(inv.total) - paid));

  const period = inv.billing_month
    ? monthRange(inv.billing_month)
    : (() => {
      const s = lines.map(l => String(l.period_start || '').slice(0, 10)).filter(Boolean).sort();
      const e = lines.map(l => String(l.period_end || '').slice(0, 10)).filter(Boolean).sort();
      return s.length && e.length ? rangeText(s[0], e[e.length - 1]) : '';
    })();
  const termDays = dayDiff(inv.invoice_date, inv.due_date);

  const meta = [
    ['Invoice No.', inv.invoice_number || `Draft #${inv.id}`, true],
    ['Invoice Date', fmtDay(inv.invoice_date)],
    ['Due Date', fmtDay(inv.due_date)],
    period && ['Billing Period', period],
    inv.client?.client_number && ['Customer ID', inv.client.client_number],
  ].filter(Boolean);

  const companyLines = [
    co.address && co.address.replace(/\s*\n\s*/g, ', '),
    [co.email, co.phone].filter(Boolean).join(' · '),
    co.website,
    [co.ntn && `NTN: ${co.ntn}`, co.strn && `STRN: ${co.strn}`].filter(Boolean).join(' · '),
  ].filter(Boolean);

  const billTo = [
    inv.bill_contact && inv.bill_client_type !== 'individual' && `Attn: ${inv.bill_contact}`,
    [inv.bill_address, [inv.bill_city, inv.bill_province].filter(Boolean).join(', ')].filter(Boolean).join(', '),
    [inv.bill_email, inv.bill_phone].filter(Boolean).join(' · '),
    taxIds({ type: inv.bill_client_type, ntn: inv.bill_ntn, strn: inv.bill_strn, cnic: inv.bill_cnic }).replace(/ {3}/g, ' · '),
  ].filter(Boolean);

  const terms = [
    termDays != null && termDays >= 0
      ? `Payment due ${termDays === 0 ? 'on receipt' : `within ${termDays} day${termDays === 1 ? '' : 's'} of invoice date`}. Please quote the invoice number with your payment.`
      : 'Please quote the invoice number with your payment.',
    ...String(inv.notes || '').split('\n').map(s => s.trim()).filter(Boolean),
  ];

  const th = 'py-1.5 px-2 font-medium';
  const td = 'py-1.5 px-2 align-top';

  return (
    <div className="invoice-print min-h-full bg-zinc-200 py-6 print:p-0 print:bg-white">
      <style>{`
        @page { size: A4; margin: 12mm; }
        @media print {
          html, body, #root { height: auto !important; background: #fff !important; }
          .no-print { display: none !important; }
          .sheet { box-shadow: none !important; margin: 0 !important; width: auto !important; min-height: 0 !important; padding: 0 !important; }
        }
        .invoice-print, .invoice-print * { color-scheme: light; -webkit-print-color-adjust: exact; print-color-adjust: exact; }
        .invoice-print .num { font-variant-numeric: tabular-nums; }
      `}</style>
      <div className="no-print max-w-[210mm] mx-auto mb-3 px-2 flex flex-wrap items-center justify-between gap-2">
        <button type="button" onClick={back} className="text-sm text-indigo-700 hover:underline cursor-pointer">← Back to the invoice</button>
        <div className="flex items-center gap-3">
          <span className="text-xs text-zinc-600">To make a PDF, choose "Save as PDF" as the printer.</span>
          <button type="button" onClick={() => window.print()} className="px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-700 text-white text-sm font-semibold cursor-pointer">Print / Save as PDF</button>
        </div>
      </div>

      <div className="sheet relative max-w-[210mm] min-h-[297mm] mx-auto bg-white text-[#0f172a] shadow-lg p-[14mm] text-[11px] leading-[1.5]">
        {stamp && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center overflow-hidden">
            <div className="text-[110px] font-black tracking-widest text-rose-500/15 -rotate-30 select-none">{stamp}</div>
          </div>
        )}

        {/* Top: the billing company on the left, the invoice on the right */}
        <div className="flex justify-between items-start gap-8">
          <div className="min-w-0 flex items-start gap-3">
            {co.has_logo && (
              <CompanyLogo src={`/api/invoices/${inv.id}/logo`} version={inv.updated_at} alt={co.company_name}
                className="block h-[13mm] w-auto max-w-[40mm] object-contain object-left shrink-0" />
            )}
            <div className="min-w-0">
              <div className="text-[17px] font-bold leading-tight">{co.company_name || 'Your company name'}</div>
              <div className="mt-1 text-slate-500 text-[10.5px] leading-[1.55]">
                {companyLines.map((t, i) => <div key={i}>{t}</div>)}
              </div>
            </div>
          </div>
          <div className="shrink-0 text-right">
            <div className="text-[22px] font-semibold tracking-[0.18em] leading-none">INVOICE</div>
            {Number(inv.tax_amount) > 0 && <div className="mt-1 text-[10px] text-slate-500">Sales Tax Invoice</div>}
            <table className="ml-auto mt-3 text-[10.5px]">
              <tbody>
                {meta.map(([k, v, strong]) => (
                  <tr key={k}>
                    <td className="pr-4 py-[1px] text-left text-slate-500">{k}</td>
                    <td className={`py-[1px] text-right whitespace-nowrap ${strong ? 'font-bold' : 'font-medium'}`}>{v}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        <div className="mt-4 border-t border-slate-200" />

        {/* Bill to + amount due */}
        <div className="mt-4 flex gap-4" style={{ breakInside: 'avoid' }}>
          <div className="flex-1 min-w-0 rounded-lg border border-slate-200 px-4 py-3">
            <Label>Bill to</Label>
            <div className="mt-1 text-[13px] font-bold">{inv.bill_name}</div>
            <div className="text-slate-600">{billTo.map((t, i) => <div key={i}>{t}</div>)}</div>
          </div>
          <div className="w-[62mm] shrink-0 rounded-lg bg-[#eff4ff] px-4 py-3 flex flex-col justify-center">
            <Label>{paid > 0 ? 'Balance due' : 'Amount due'}</Label>
            <div className="mt-1 text-[20px] font-bold num leading-tight">{cur} {money(paid > 0 ? balance : inv.total)}</div>
            <div className="mt-0.5 text-slate-500">Due by {fmtDay(inv.due_date)}</div>
          </div>
        </div>

        {/* Lines */}
        <table className="mt-4 w-full border-collapse">
          <thead>
            <tr className="text-[10px] text-slate-500 border-b border-slate-300">
              <th className={`${th} text-left w-9`}>#</th>
              <th className={`${th} text-left`}>Description</th>
              <th className={`${th} text-right w-10`}>Qty</th>
              <th className={`${th} text-left w-16`}>Unit</th>
              <th className={`${th} text-right w-24`}>Rate ({cur})</th>
              <th className={`${th} text-right w-28`}>Amount ({cur})</th>
            </tr>
          </thead>
          {groups.map(g => (
            <tbody key={g.name || 'all'}>
              {g.name && (
                <tr className="bg-[#f5f8ff]" style={{ breakInside: 'avoid', breakAfter: 'avoid' }}>
                  <td colSpan={5} className="py-1.5 px-2 text-[10px] font-semibold uppercase tracking-[0.1em] text-[#2563eb]">{g.letter} · {g.name}</td>
                  <td className="py-1.5 px-2 text-right font-semibold text-[#2563eb] num">{money(g.total)}</td>
                </tr>
              )}
              {g.lines.map((l, i) => {
                const sub = [l.reference, linePeriod(l)].filter(Boolean).join(' · ');
                const tx = (l.taxes || []).length ? `Tax: ${l.taxes.map(t => `${t.kind === 'deduct' ? 'less ' : ''}${taxShort(t)}`).join(', ')}` : '';
                return (
                  <tr key={l.id} className="border-b border-slate-100" style={{ breakInside: 'avoid' }}>
                    <td className={`${td} text-slate-400`}>{g.letter}{i + 1}</td>
                    <td className={td}>
                      <div className="font-semibold">{l.description}</div>
                      {sub && <div className="text-[10px] text-slate-500">{sub}</div>}
                      {tx && <div className="text-[9.5px] text-slate-400">{tx}</div>}
                    </td>
                    <td className={`${td} text-right num`}>{Number(l.quantity)}</td>
                    <td className={`${td} text-slate-600`}>{unitOf(l)}</td>
                    <td className={`${td} text-right num`}>{money(l.rate)}</td>
                    <td className={`${td} text-right font-semibold num`}>{money(l.amount)}</td>
                  </tr>
                );
              })}
            </tbody>
          ))}
        </table>

        {/* Payment details + amount in words | totals */}
        <div className="mt-4 flex justify-between gap-6" style={{ breakInside: 'avoid' }}>
          <div className="flex-1 min-w-0">
            {co.bank_details && (
              <div className="rounded-lg border border-slate-200 px-4 py-3">
                <Label>Payment details</Label>
                <div className="mt-1 whitespace-pre-line text-slate-700">{co.bank_details}</div>
              </div>
            )}
            <div className={`${co.bank_details ? 'mt-3' : ''} text-slate-700`}>
              <span className="font-semibold text-[#0f172a]">Amount in words: </span>{amountInWords(inv.total, cur)}
            </div>
          </div>
          <div className="w-[72mm] shrink-0">
            <table className="w-full num">
              <tbody>
                {groups.length > 1 && groups.map(g => (
                  <tr key={g.name} className="border-b border-slate-100">
                    <td className="py-1 text-slate-500">{g.letter} · {g.name}</td>
                    <td className="py-1 text-right">{money(g.total)}</td>
                  </tr>
                ))}
                <tr className="border-b border-slate-100"><td className="py-1 font-semibold">Subtotal</td><td className="py-1 text-right font-semibold">{money(discount ? gross : inv.subtotal)}</td></tr>
                {discount !== 0 && (
                  <tr className="border-b border-slate-100"><td className="py-1 text-slate-500">Discount</td><td className="py-1 text-right">({money(-discount)})</td></tr>
                )}
                {taxLines.map((t, k) => (
                  <tr key={k} className="border-b border-slate-100">
                    <td className="py-1 text-slate-500">{t.kind === 'deduct' ? 'Less: ' : ''}{t.name} @ {pctText(t.rate)}</td>
                    <td className="py-1 text-right">{t.kind === 'deduct' ? `(${money(t.amount)})` : money(t.amount)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="mt-2 rounded-lg bg-[#0f172a] text-white px-4 py-2.5 flex items-center justify-between">
              <span className="font-semibold">Total Due ({cur})</span>
              <span className="text-[16px] font-bold num">{money(inv.total)}</span>
            </div>
            {paid > 0 && (
              <table className="w-full num mt-1">
                <tbody>
                  <tr><td className="py-0.5 text-slate-500">Paid</td><td className="py-0.5 text-right">({money(paid)})</td></tr>
                  <tr><td className="py-0.5 font-semibold">Balance due</td><td className="py-0.5 text-right font-semibold">{money(balance)}</td></tr>
                </tbody>
              </table>
            )}
          </div>
        </div>

        {/* Terms & notes */}
        <div className="mt-5" style={{ breakInside: 'avoid' }}>
          <div className="font-semibold">Terms &amp; Notes</div>
          <ol className="mt-0.5 text-slate-600 list-decimal pl-4">
            {terms.map((t, i) => <li key={i}>{t}</li>)}
          </ol>
        </div>

        <div className="mt-5 pt-2 border-t border-slate-200 text-center text-[9.5px] text-slate-400" style={{ breakInside: 'avoid' }}>
          {co.invoice_footer && <div className="text-[10px] text-slate-500 whitespace-pre-line">{co.invoice_footer}</div>}
          <div>This is a computer-generated invoice.</div>
        </div>
      </div>
    </div>
  );
};
