import React from 'react';
import { pctText } from '../lib/format';

// Tick the taxes that apply. taxes: all taxes (from /api/taxes?status=all);
// inactive ones only show when already chosen. companyId: only that
// company's taxes (services and invoices use their company's taxes).
// companies: when given (products, which any company may bill), each tax
// shows its company's name.
export const TaxPicker = ({ taxes, value = [], onChange, disabled, compact, companyId, companies }) => {
  const chosen = new Set((value || []).map(Number));
  const list = (taxes || []).filter(t => (t.active || chosen.has(t.id)) && (!companyId || t.company_id === Number(companyId)));
  const coName = (id) => (companies && companies.length > 1 ? companies.find(c => c.id === id)?.name : '');
  if (list.length === 0) {
    return <div className="text-xs text-zinc-500">No taxes set up {companyId ? 'for this company ' : ''}yet (Taxes page).</div>;
  }
  const toggle = (id) => {
    const next = new Set(chosen);
    if (next.has(id)) next.delete(id); else next.add(id);
    onChange(list.filter(t => next.has(t.id)).map(t => t.id));
  };
  return (
    <div className={`flex flex-wrap ${compact ? 'gap-1' : 'gap-1.5'}`}>
      {list.map(t => {
        const on = chosen.has(t.id);
        return (
          <button key={t.id} type="button" disabled={disabled} onClick={() => toggle(t.id)} aria-pressed={on}
            title={`${t.name} ${pctText(t.rate)}${t.kind === 'deduct' ? ' (deducted)' : ''}${t.compound ? ' (on amount + earlier taxes)' : ''}${coName(t.company_id) ? ` · ${coName(t.company_id)}` : ''}`}
            className={`inline-flex items-center gap-1 rounded-full border ${compact ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-1 text-xs'} font-medium cursor-pointer disabled:cursor-default disabled:opacity-70 ${on
              ? (t.kind === 'deduct' ? 'bg-amber-100 border-amber-400 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-700' : 'bg-indigo-600 border-indigo-600 text-white')
              : 'border-zinc-300 dark:border-zinc-700 text-zinc-600 dark:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-800'}`}>
            {t.kind === 'deduct' && '−'}{t.short_name || t.name} {pctText(t.rate)}{!t.active && ' (inactive)'}
            {coName(t.company_id) && <span className="opacity-70">· {coName(t.company_id)}</span>}
          </button>
        );
      })}
    </div>
  );
};
