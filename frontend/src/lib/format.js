// Display helpers.

export const formatMoney = (n, currency = 'PKR') => {
  if (n === null || n === undefined || n === '' || Number.isNaN(Number(n))) return '';
  const v = Number(n);
  const s = Math.abs(v).toLocaleString('en-PK', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  return `${v < 0 ? '−' : ''}${currency} ${s}`;
};

// Dates stored as plain days ("2026-10-28" or midnight UTC): read without
// letting the time zone move the day.
export const dayOf = (v) => (v ? String(v).slice(0, 10) : '');
export const fmtDay = (v) => {
  const d = dayOf(v);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(d)) return '';
  const [y, m, day] = d.split('-').map(Number);
  return new Date(y, m - 1, day).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' });
};
export const fmtDateTime = (v) => (v ? new Date(v).toLocaleString('en-GB', {
  day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit',
}) : '');

export const todayISO = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};

export const PROVINCES = [
  'Punjab', 'Sindh', 'Khyber Pakhtunkhwa', 'Balochistan', 'Islamabad Capital Territory', 'Gilgit-Baltistan', 'Azad Jammu & Kashmir',
];

// ---- services (step 2) ----

export const CHARGE_TYPES = { monthly: 'Monthly (MRC)', one_time: 'One-time', annual: 'Per annum' };
export const CHARGE_SHORT = { monthly: '/ month', one_time: 'once', annual: '/ year' };

// What a service's dates and status mean today (worked out by the server).
export const SERVICE_STATES = {
  active: ['Active', 'green'],
  upcoming: ['Starts later', 'indigo'],
  ending: ['Ending', 'amber'],
  suspended: ['Suspended', 'amber'],
  ended: ['Ended', 'zinc'],
  to_bill: ['To bill', 'indigo'],
  billed: ['Billed', 'zinc'],
};

const parseDay = (s) => {
  const d = dayOf(s);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(d)) return null;
  const [y, m, day] = d.split('-').map(Number);
  return { y, m, day };
};
const daysIn = (y, m) => new Date(y, m, 0).getDate();
const monthName = (y, m) => new Date(y, m - 1, 1).toLocaleDateString('en-GB', { month: 'long', year: 'numeric' });
const round2 = (v) => Math.round(v * 100) / 100;

// Part-month charges, the same rule invoicing uses: monthly amount × days
// used ÷ days in that month. Returns the first and (if ending) last month
// when they are not full months.
export const partMonths = (amount, start, end) => {
  const s = parseDay(start);
  if (!s || !Number.isFinite(amount)) return [];
  const e = parseDay(end);
  if (e && (e.y * 10000 + e.m * 100 + e.day) < (s.y * 10000 + s.m * 100 + s.day)) return [];
  const out = [];
  const sameMonth = e && e.y === s.y && e.m === s.m;
  const dim = daysIn(s.y, s.m);
  const firstLast = sameMonth ? e.day : dim;
  if (s.day !== 1 || firstLast !== dim) {
    const days = firstLast - s.day + 1;
    out.push({ label: monthName(s.y, s.m), days, of: dim, amount: round2(amount * days / dim), which: sameMonth ? 'only' : 'first' });
  }
  if (e && !sameMonth) {
    const edim = daysIn(e.y, e.m);
    if (e.day !== edim) out.push({ label: monthName(e.y, e.m), days: e.day, of: edim, amount: round2(amount * e.day / edim), which: 'last' });
  }
  return out;
};

// ---- invoices (step 3) ----

export const INVOICE_STATUS = { draft: ['Draft', 'amber'], issued: ['Issued', 'indigo'], cancelled: ['Cancelled', 'zinc'] };

// "2026-11-01" → "November 2026"
export const fmtMonth = (v) => {
  const p = parseDay(v);
  return p ? monthName(p.y, p.m) : '';
};

// "2026-11" for a date (default: this month)
export const monthISO = (v) => (v ? dayOf(v).slice(0, 7) : todayISO().slice(0, 7));

// What period an invoice line bills, in words.
export const linePeriod = (l) => {
  const s = parseDay(l.period_start);
  const e = parseDay(l.period_end);
  if (!s || !e) return '';
  if (l.charge_type === 'one_time') return fmtDay(l.period_start);
  const full = s.day === 1 && e.y === s.y && e.m === s.m && e.day === daysIn(e.y, e.m);
  if (full) return monthName(s.y, s.m);
  if (s.y === e.y && s.m === e.m && s.day === e.day) {
    return `${fmtDay(l.period_start)}${l.prorate_days ? ` (1 of ${l.prorate_of} days)` : ''}`;
  }
  const short = (p) => new Date(p.y, p.m - 1, p.day).toLocaleDateString('en-GB', { day: '2-digit', month: 'short' });
  const range = s.y === e.y ? `${short(s)} – ${fmtDay(l.period_end)}` : `${fmtDay(l.period_start)} – ${fmtDay(l.period_end)}`;
  return l.prorate_days ? `${range} (${l.prorate_days} of ${l.prorate_of} days)` : range;
};

// The same amount rule as the server: quantity × rate, by days for a part month.
export const lineAmount = (qty, rate, days, of) => {
  let v = Number(qty) * Number(rate);
  if (days > 0 && of > 0) v = v * days / of;
  return Math.round(v * 100) / 100;
};

// Amount in words for the printed invoice: "Seventy-Four Thousand Eight
// Hundred Twenty Rupees and Fifty Paisa Only".
const ONES = ['', 'One', 'Two', 'Three', 'Four', 'Five', 'Six', 'Seven', 'Eight', 'Nine', 'Ten', 'Eleven', 'Twelve',
  'Thirteen', 'Fourteen', 'Fifteen', 'Sixteen', 'Seventeen', 'Eighteen', 'Nineteen'];
const TENS = ['', '', 'Twenty', 'Thirty', 'Forty', 'Fifty', 'Sixty', 'Seventy', 'Eighty', 'Ninety'];
const under1000 = (n) => {
  const h = Math.floor(n / 100);
  const r = n % 100;
  const parts = [];
  if (h) parts.push(`${ONES[h]} Hundred`);
  if (r) parts.push(r < 20 ? ONES[r] : TENS[Math.floor(r / 10)] + (r % 10 ? `-${ONES[r % 10]}` : ''));
  return parts.join(' ');
};
const intWords = (n) => {
  if (n === 0) return 'Zero';
  const scales = [[1e9, 'Billion'], [1e6, 'Million'], [1e3, 'Thousand']];
  const parts = [];
  let rest = n;
  for (const [v, name] of scales) {
    if (rest >= v) {
      parts.push(`${intWords(Math.floor(rest / v))} ${name}`);
      rest %= v;
    }
  }
  if (rest) parts.push(under1000(rest));
  return parts.join(' ');
};
export const amountInWords = (amount, currency = 'PKR') => {
  const v = Math.round(Math.abs(Number(amount) || 0) * 100);
  const whole = Math.floor(v / 100);
  const cents = v % 100;
  const unit = currency === 'PKR' ? ['Rupees', 'Paisa'] : [currency, 'Cents'];
  return `${intWords(whole)} ${unit[0]}${cents ? ` and ${intWords(cents)} ${unit[1]}` : ''} Only`;
};

// ---- payments (step 4) ----

export const PAYMENT_STATUS = {
  pending: ['Waiting for verification', 'amber'], verified: ['Verified', 'green'],
  rejected: ['Rejected', 'red'], reversed: ['Reversed', 'zinc'],
};
export const PAYMENT_METHODS = {
  bank_transfer: 'Bank transfer / IBFT', cheque: 'Cheque', pay_order: 'Pay order / demand draft',
  cash: 'Cash', online: 'Online / card', other: 'Other',
};

// An issued invoice's payment state: paid / partly paid / overdue / unpaid.
export const payState = (inv, today = todayISO()) => {
  if (!inv || inv.status !== 'issued') return null;
  const balance = Math.round((Number(inv.total) - Number(inv.amount_paid || 0)) * 100) / 100;
  if (balance <= 0) return ['Paid', 'green', 0];
  const overdue = dayOf(inv.due_date) < today;
  if (Number(inv.amount_paid) > 0) return [overdue ? 'Partly paid · overdue' : 'Partly paid', overdue ? 'red' : 'amber', balance];
  return [overdue ? 'Overdue' : 'Unpaid', overdue ? 'red' : 'zinc', balance];
};

// ---- contracts and service levels ----

// What a contract means today (worked out by the server).
export const CONTRACT_STATES = {
  draft: ['Draft', 'zinc'],
  upcoming: ['Starts later', 'indigo'],
  active: ['Active', 'green'],
  due: ['Notice due', 'amber'],
  expired: ['Expired', 'red'],
  terminated: ['Terminated', 'zinc'],
};

export const SUPPORT_HOURS = { '24x7': '24 × 7', business: 'Business hours', custom: 'Other' };

// 90 → "1h 30m", 1440 → "1 day", 0 → "—"
export const minutesText = (m) => {
  m = Number(m) || 0;
  if (m <= 0) return '—';
  if (m % 1440 === 0) return `${m / 1440} day${m === 1440 ? '' : 's'}`;
  if (m % 60 === 0) return `${m / 60} hour${m === 60 ? '' : 's'}`;
  if (m > 60) return `${Math.floor(m / 60)}h ${m % 60}m`;
  return `${m} min`;
};

export const pctText = (v) => `${Number(v || 0).toLocaleString('en-GB', { maximumFractionDigits: 3 })}%`;

// One line describing a service level's targets.
export const slaTargets = (p) => {
  if (!p) return '';
  const parts = [];
  if (Number(p.uptime_target) > 0) parts.push(`${pctText(p.uptime_target)} uptime`);
  if (p.response_minutes > 0) parts.push(`response ${minutesText(p.response_minutes)}`);
  if (p.restore_minutes > 0) parts.push(`restore ${minutesText(p.restore_minutes)}`);
  parts.push(p.support_hours === 'custom' ? (p.support_note || 'custom hours') : SUPPORT_HOURS[p.support_hours] || p.support_hours);
  return parts.join(' · ');
};

// The credit a month's downtime earns under a service level: uptime % for
// the month, then the step with the lowest "below %" that the uptime falls
// under (the biggest credit), capped. monthly = the service's monthly charge.
export const slaCredit = (plan, monthly, downtimeMinutes, daysInMonth) => {
  const total = daysInMonth * 24 * 60;
  const down = Math.min(Math.max(Number(downtimeMinutes) || 0, 0), total);
  const uptime = Math.round((1 - down / total) * 100000) / 1000;
  const tiers = [...(plan?.credit_tiers || [])].sort((a, b) => a.below_percent - b.below_percent);
  const tier = tiers.find(t => uptime < t.below_percent) || null;
  let percent = tier ? Number(tier.credit_percent) : 0;
  const cap = Number(plan?.max_credit_percent) || 0;
  if (cap > 0 && percent > cap) percent = cap;
  const met = !(Number(plan?.uptime_target) > 0) || uptime >= Number(plan.uptime_target);
  return { uptime, met, tier, percent, amount: round2((Number(monthly) || 0) * percent / 100) };
};

export const daysInMonth = (ym) => {
  const [y, m] = String(ym).split('-').map(Number);
  return daysIn(y, m);
};

// ---- client types ----

export const CLIENT_TYPES = { company: 'Company', individual: 'Individual' };

// Digits typed into a CNIC box → 12345-1234567-1 (as far as typed).
export const formatCNIC = (v) => {
  const d = String(v || '').replace(/\D/g, '').slice(0, 13);
  if (d.length <= 5) return d;
  if (d.length <= 12) return `${d.slice(0, 5)}-${d.slice(5)}`;
  return `${d.slice(0, 5)}-${d.slice(5, 12)}-${d.slice(12)}`;
};

// The tax / ID numbers to print for a client or invoice, ID first for a person.
export const taxIds = ({ type, ntn, strn, cnic }) => (type === 'individual'
  ? [cnic && `CNIC: ${cnic}`, ntn && `NTN: ${ntn}`, strn && `STRN: ${strn}`]
  : [ntn && `NTN: ${ntn}`, strn && `STRN: ${strn}`, cnic && `CNIC: ${cnic}`]).filter(Boolean).join('   ');

// ---- taxes ----

export const TAX_KINDS = { add: 'Added', deduct: 'Deducted' };

// "PST 16%" — short name if given.
export const taxShort = (t) => `${t.short_name || t.name} ${pctText(t.rate)}`;

// The same rule as the server (recalcInvoice): per line, in order, each tax
// is rate % of the line amount (compound: plus the taxes added before it);
// each tax's total is rounded once. lines: [{ amount, taxes: [tax-like] }].
export const invoiceTaxes = (lines, legacyPercent = 0) => {
  const r2 = (v) => Math.round(v * 100) / 100;
  let sub = 0;
  const sums = new Map();
  lines.forEach((l) => {
    const amount = Number(l.amount) || 0;
    sub += amount;
    const ts = [...(l.taxes || [])].sort((a, b) => (a.order - b.order) || ((a.tax_id ?? a.id) - (b.tax_id ?? b.id)));
    let added = 0;
    ts.forEach((t) => {
      const base = amount + (t.compound ? added : 0);
      const amt = base * Number(t.rate) / 100;
      if (t.kind === 'add') added += amt;
      const key = `${t.tax_id ?? t.id}|${t.rate}|${t.kind}|${t.name}`;
      const s = sums.get(key) || { ...t, base: 0, amount: 0 };
      s.base += base;
      s.amount += amt;
      sums.set(key, s);
    });
  });
  const subtotal = r2(sub);
  const taxLines = [...sums.values()].map(s => ({ ...s, base: r2(s.base), amount: r2(s.amount) }));
  if (Number(legacyPercent) > 0) {
    taxLines.push({ name: 'Sales tax', rate: Number(legacyPercent), kind: 'add', order: 1000, base: subtotal, amount: r2(subtotal * legacyPercent / 100) });
  }
  taxLines.sort((a, b) => (a.kind === b.kind ? a.order - b.order : a.kind === 'add' ? -1 : 1));
  const added = r2(taxLines.filter(t => t.kind === 'add').reduce((s, t) => s + t.amount, 0));
  const deducted = r2(taxLines.filter(t => t.kind === 'deduct').reduce((s, t) => s + t.amount, 0));
  return { subtotal, taxLines, added, deducted, total: r2(subtotal + added - deducted) };
};
