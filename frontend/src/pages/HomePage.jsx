import React, { useEffect, useState } from 'react';
import { LayoutDashboard, Building2, Network, Package, FileText, Wallet, AlertTriangle, Settings, Clock, Scale } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth, navigate } from '../lib/auth';
import { PageHeader, Card, Button } from '../components/ui';
import { formatMoney, fmtMonth, monthISO } from '../lib/format';

// Dashboard: what's owed, what's late, what came in, what needs doing.
// Detailed reports with Excel downloads: the Reports page.

const Tile = ({ icon: Icon, label, value, sub, to, tone = '' }) => (
  <button type="button" onClick={() => to && navigate(to)} className="text-left cursor-pointer">
    <Card className="p-4 h-full hover:border-indigo-400">
      <div className="text-xs font-semibold uppercase tracking-wider text-zinc-500 flex items-center gap-1.5"><Icon className="w-4 h-4" /> {label}</div>
      <div className={`text-2xl font-bold mt-1 ${tone}`}>{value ?? '—'}</div>
      {sub && <div className="text-xs text-zinc-500 mt-0.5">{sub}</div>}
    </Card>
  </button>
);

export const HomePage = () => {
  const { user, can } = useAuth();
  const [settings, setSettings] = useState(null);
  const [dash, setDash] = useState(null);
  const [clients, setClients] = useState(null);
  const [services, setServices] = useState(null);
  const [products, setProducts] = useState(null);
  const [drafts, setDrafts] = useState(0);

  useEffect(() => {
    api('/api/settings').then(d => setSettings(d.settings)).catch(() => {});
    if (!can('view_billing')) return;
    api('/api/dashboard').then(d => setDash(d.dashboard)).catch(() => {});
    api('/api/clients?page_size=1').then(d => setClients(d.total)).catch(() => {});
    api('/api/subscriptions?page_size=1').then(d => setServices(d.totals)).catch(() => {});
    api('/api/products').then(d => setProducts(d.products.length)).catch(() => {});
    api('/api/invoices?status=draft&page_size=1').then(d => setDrafts(d.total)).catch(() => {});
  }, [can]);

  const needsCompany = settings && !settings.company_name;
  const month = fmtMonth(`${monthISO()}-01`);
  return (
    <>
      <PageHeader icon={LayoutDashboard} title={`Welcome, ${user.name}`} subtitle="Clients, invoices and payments." />
      {needsCompany && can('manage_settings') && (
        <Card className="p-4 mb-4 border-amber-300 dark:border-amber-800 bg-amber-50/60 dark:bg-amber-950/20">
          <p className="text-sm flex items-start gap-2"><AlertTriangle className="w-4 h-4 mt-0.5 text-amber-600" />
            Add your company's details (name, NTN, address, bank details). They are printed on every invoice.</p>
          <Button className="mt-3" icon={Settings} onClick={() => navigate('settings')}>Company settings</Button>
        </Card>
      )}
      {can('view_billing') && (
        <>
          {(dash?.pending_count > 0 || drafts > 0 || dash?.contracts_due > 0 || dash?.contracts_expired > 0) && (
            <div className="flex flex-col gap-2 mb-4">
              {(dash?.contracts_due > 0 || dash?.contracts_expired > 0) && (
                <Card className="p-3 text-sm border-amber-300 dark:border-amber-800 bg-amber-50/60 dark:bg-amber-950/20 flex flex-wrap items-center justify-between gap-2">
                  <span>
                    {dash.contracts_due > 0 && <><b>{dash.contracts_due}</b> contract{dash.contracts_due === 1 ? '' : 's'} past the notice date (renew or let end){dash.contracts_expired > 0 ? '; ' : '.'}</>}
                    {dash.contracts_expired > 0 && <><b>{dash.contracts_expired}</b> expired without renewal.</>}
                  </span>
                  <Button variant="secondary" onClick={() => navigate('contracts?state=due')}>Review</Button>
                </Card>
              )}
              {dash?.pending_count > 0 && (
                <Card className="p-3 text-sm border-amber-300 dark:border-amber-800 bg-amber-50/60 dark:bg-amber-950/20 flex flex-wrap items-center justify-between gap-2">
                  <span><b>{dash.pending_count}</b> payment{dash.pending_count === 1 ? '' : 's'} waiting for verification ({formatMoney(dash.pending_amount)}).</span>
                  <Button variant="secondary" onClick={() => navigate('payments')}>{can('verify_payments') ? 'Verify' : 'View'}</Button>
                </Card>
              )}
              {drafts > 0 && can('manage_billing') && (
                <Card className="p-3 text-sm border-amber-300 dark:border-amber-800 bg-amber-50/60 dark:bg-amber-950/20 flex flex-wrap items-center justify-between gap-2">
                  <span><b>{drafts}</b> draft invoice{drafts === 1 ? '' : 's'} waiting to be issued.</span>
                  <Button variant="secondary" onClick={() => navigate('invoices')}>Review</Button>
                </Card>
              )}
            </div>
          )}
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            <Tile icon={Scale} label="Receivable" to="clients" value={dash && formatMoney(dash.receivable)}
              sub="opening balances + invoices − verified payments" />
            <Tile icon={Clock} label="Overdue" to="invoices" value={dash && formatMoney(dash.overdue)} tone={dash?.overdue > 0 ? 'text-rose-600' : ''}
              sub={dash && `${dash.overdue_count} invoice${dash.overdue_count === 1 ? '' : 's'} past due`} />
            <Tile icon={Wallet} label={`Collected · ${month}`} to="payments" value={dash && formatMoney(dash.collected_month)}
              sub="verified, incl. tax deducted" tone="text-emerald-600" />
            <Tile icon={FileText} label={`Invoiced · ${month}`} to="invoices" value={dash && formatMoney(dash.invoiced_month)} sub="issued, with tax" />
            <Tile icon={Network} label="Monthly recurring (MRC)" to="services" value={services && formatMoney(services.monthly)}
              sub={services && `${services.count} service${services.count === 1 ? '' : 's'} billing now, before tax`} />
            <Tile icon={Building2} label="Clients" to="clients" value={clients} sub="active and inactive" />
            <Tile icon={Package} label="Products" to="products" value={products} sub="on sale" />
          </div>
        </>
      )}
      {can('view_reports') && <p className="text-xs text-zinc-500 mt-6">More detail, with Excel downloads: <a href="#/reports/ageing" className="text-indigo-600 dark:text-indigo-400 hover:underline">ageing</a>, <a href="#/reports/billed-collected" className="text-indigo-600 dark:text-indigo-400 hover:underline">billed vs collected</a>, <a href="#/reports/invoices" className="text-indigo-600 dark:text-indigo-400 hover:underline">invoice register</a>.</p>}
    </>
  );
};
