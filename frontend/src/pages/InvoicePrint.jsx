import React, { useEffect, useState } from 'react';
import { api } from '../lib/api';
import { fmtDay, fmtMonth, linePeriod, amountInWords, taxIds, pctText, taxShort } from '../lib/format';
import { Spinner, ErrorText } from '../components/ui';
import { CompanyLogo } from '../components/CompanyLogo';

// The printed invoice (#/print/12): A4, always light, no app around it.
// "Print / Save as PDF" uses the browser's print window, where "Save as
// PDF" makes the PDF.

const money = (v) => Number(v || 0).toLocaleString('en-PK', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

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

  return (
    <div className="invoice-print min-h-full bg-zinc-200 py-6 print:p-0 print:bg-white">
      <style>{`
        @page { size: A4; margin: 12mm; }
        @media print {
          html, body, #root { height: auto !important; background: #fff !important; }
          .no-print { display: none !important; }
          .sheet { box-shadow: none !important; margin: 0 !important; width: auto !important; min-height: 0 !important; padding: 0 !important; }
        }
        .invoice-print, .invoice-print * { color-scheme: light; }
      `}</style>
      <div className="no-print max-w-[210mm] mx-auto mb-3 px-2 flex flex-wrap items-center justify-between gap-2">
        <button type="button" onClick={back} className="text-sm text-indigo-700 hover:underline cursor-pointer">← Back to the invoice</button>
        <div className="flex items-center gap-3">
          <span className="text-xs text-zinc-600">To make a PDF, choose "Save as PDF" as the printer.</span>
          <button type="button" onClick={() => window.print()} className="px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-700 text-white text-sm font-semibold cursor-pointer">Print / Save as PDF</button>
        </div>
      </div>

      <div className="sheet relative max-w-[210mm] min-h-[297mm] mx-auto bg-white text-zinc-900 shadow-lg p-[14mm] text-[12px] leading-snug">
        {stamp && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center overflow-hidden">
            <div className="text-[110px] font-black tracking-widest text-rose-500/15 -rotate-30 select-none">{stamp}</div>
          </div>
        )}

        {/* Company (the one that bills: its logo and details) and title */}
        <div className="flex justify-between gap-6 pb-4 border-b-2 border-zinc-900">
          <div className="min-w-0">
            {co.has_logo && (
              <CompanyLogo src={`/api/invoices/${inv.id}/logo`} version={inv.updated_at} alt={co.company_name}
                className="block max-h-[22mm] max-w-[70mm] object-contain object-left mb-2" />
            )}
            <div className="text-xl font-bold">{co.company_name || 'Your company name'}</div>
            <div className="whitespace-pre-line text-zinc-700">{co.address}</div>
            <div className="text-zinc-700">{[co.phone, co.email, co.website].filter(Boolean).join(' · ')}</div>
            <div className="text-zinc-700">{[co.ntn && `NTN: ${co.ntn}`, co.strn && `STRN: ${co.strn}`].filter(Boolean).join('   ')}</div>
          </div>
          <div className="text-right shrink-0">
            <div className="text-2xl font-black tracking-wide">{Number(inv.tax_amount) > 0 ? 'SALES TAX INVOICE' : 'INVOICE'}</div>
            <table className="ml-auto mt-2 text-left">
              <tbody>
                <tr><td className="pr-3 text-zinc-500">Invoice no.</td><td className="font-mono font-bold">{inv.invoice_number || `Draft #${inv.id}`}</td></tr>
                <tr><td className="pr-3 text-zinc-500">Invoice date</td><td>{fmtDay(inv.invoice_date)}</td></tr>
                <tr><td className="pr-3 text-zinc-500">Due date</td><td className="font-semibold">{fmtDay(inv.due_date)}</td></tr>
                {inv.billing_month && <tr><td className="pr-3 text-zinc-500">Billing month</td><td>{fmtMonth(inv.billing_month)}</td></tr>}
                {inv.client?.client_number && <tr><td className="pr-3 text-zinc-500">Customer no.</td><td className="font-mono">{inv.client.client_number}</td></tr>}
              </tbody>
            </table>
          </div>
        </div>

        {/* Bill to */}
        <div className="mt-4 mb-4">
          <div className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Bill to</div>
          <div className="text-[14px] font-bold">{inv.bill_name}</div>
          {inv.bill_contact && inv.bill_client_type !== 'individual' && <div>Attn: {inv.bill_contact}</div>}
          <div className="whitespace-pre-line">{[inv.bill_address, [inv.bill_city, inv.bill_province].filter(Boolean).join(', ')].filter(Boolean).join('\n')}</div>
          <div className="text-zinc-700">{[inv.bill_phone, inv.bill_email].filter(Boolean).join(' · ')}</div>
          <div className="text-zinc-700">{taxIds({ type: inv.bill_client_type, ntn: inv.bill_ntn, strn: inv.bill_strn, cnic: inv.bill_cnic })}</div>
        </div>

        {/* Lines */}
        <table className="w-full border-collapse">
          <thead>
            <tr className="bg-zinc-900 text-white text-[11px]">
              <th className="py-1.5 px-2 text-left w-8">#</th>
              <th className="py-1.5 px-2 text-left">Description</th>
              <th className="py-1.5 px-2 text-right w-14">Qty</th>
              <th className="py-1.5 px-2 text-right w-28">Rate</th>
              <th className="py-1.5 px-2 text-right w-32">Amount ({cur})</th>
            </tr>
          </thead>
          <tbody>
            {(inv.lines || []).map((l, i) => (
              <tr key={l.id} className="border-b border-zinc-200 align-top" style={{ breakInside: 'avoid' }}>
                <td className="py-1.5 px-2 text-zinc-500">{i + 1}</td>
                <td className="py-1.5 px-2">
                  <div className="font-semibold">{l.description}</div>
                  {(l.reference || linePeriod(l)) && <div className="text-[11px] text-zinc-600">{[l.reference, linePeriod(l)].filter(Boolean).join(' · ')}</div>}
                  {(l.taxes || []).length > 0 && <div className="text-[10px] text-zinc-500">Tax: {l.taxes.map(t => `${t.kind === 'deduct' ? 'less ' : ''}${taxShort(t)}`).join(', ')}</div>}
                </td>
                <td className="py-1.5 px-2 text-right">{Number(l.quantity)}</td>
                <td className="py-1.5 px-2 text-right">{money(l.rate)}</td>
                <td className="py-1.5 px-2 text-right">{money(l.amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>

        {/* Totals */}
        <div className="mt-3 flex justify-between gap-6" style={{ breakInside: 'avoid' }}>
          <div className="text-[11px] text-zinc-700 max-w-[55%] pt-1">
            <div className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Amount in words</div>
            {amountInWords(inv.total, cur)}
          </div>
          <table className="text-right min-w-[220px]">
            <tbody>
              <tr><td className="py-0.5 pr-4 text-zinc-600">Subtotal</td><td className="py-0.5">{money(inv.subtotal)}</td></tr>
              {(inv.tax_lines?.length ? inv.tax_lines : (Number(inv.tax_amount) ? [{ name: 'Sales tax', rate: Number(inv.tax_percent), kind: 'add', amount: Number(inv.tax_amount) }] : [])).map((t, k) => (
                <tr key={k}>
                  <td className="py-0.5 pr-4 text-zinc-600">{t.kind === 'deduct' ? 'Less: ' : ''}{t.name} {pctText(t.rate)}</td>
                  <td className="py-0.5">{t.kind === 'deduct' ? `(${money(t.amount)})` : money(t.amount)}</td>
                </tr>
              ))}
              <tr className="border-t-2 border-zinc-900"><td className="py-1 pr-4 font-bold text-[14px]">Total {cur}</td><td className="py-1 font-bold text-[14px]">{money(inv.total)}</td></tr>
              {inv.status === 'issued' && Number(inv.amount_paid) > 0 && <>
                <tr><td className="py-0.5 pr-4 text-zinc-600">Paid</td><td className="py-0.5">{money(inv.amount_paid)}</td></tr>
                <tr><td className="py-0.5 pr-4 font-semibold">Balance due</td><td className="py-0.5 font-semibold">{money(Math.max(0, inv.total - inv.amount_paid))}</td></tr>
              </>}
            </tbody>
          </table>
        </div>

        {inv.notes && <div className="mt-5 whitespace-pre-line" style={{ breakInside: 'avoid' }}><div className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Notes</div>{inv.notes}</div>}

        {co.bank_details && (
          <div className="mt-5 p-3 border border-zinc-300 rounded" style={{ breakInside: 'avoid' }}>
            <div className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Please pay to</div>
            <div className="whitespace-pre-line">{co.bank_details}</div>
            <div className="mt-1 text-[11px] text-zinc-600">Please quote invoice no. <b>{inv.invoice_number || '—'}</b> with your payment. Due by {fmtDay(inv.due_date)}.</div>
          </div>
        )}

        {co.invoice_footer && <div className="mt-6 pt-2 border-t border-zinc-300 text-center text-[11px] text-zinc-600 whitespace-pre-line">{co.invoice_footer}</div>}
        <div className="mt-4 text-center text-[10px] text-zinc-400">This is a computer-generated invoice.</div>
      </div>
    </div>
  );
};
