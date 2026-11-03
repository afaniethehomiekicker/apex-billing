import React, { useCallback, useEffect, useState } from 'react';
import {
  Receipt, LayoutDashboard, Building2, Package, Network, FileText, Wallet, BarChart3, Users, Settings, ScrollText, UserCircle, LogOut, Moon, Sun, Menu, X,
  Handshake, ShieldCheck, Percent,
} from 'lucide-react';
import { api, setSessionEndedHandler, setGateHandler } from './lib/api';
import { AuthContext } from './lib/auth';
import { Spinner } from './components/ui';
import { Avatar } from './components/Avatar';
import { LoginPage } from './pages/LoginPage';
import { ChangePasswordPage } from './pages/ChangePasswordPage';
import { TwoStepSetup } from './pages/SecurityPage';
import { HomePage } from './pages/HomePage';
import { ClientsPage } from './pages/ClientsPage';
import { ProductsPage } from './pages/ProductsPage';
import { InvoicesPage } from './pages/InvoicesPage';
import { InvoicePrint } from './pages/InvoicePrint';
import { StatementPrint } from './pages/StatementPrint';
import { PaymentsPage } from './pages/PaymentsPage';
import { ReportsPage } from './pages/ReportsPage';
import { ServicesPage } from './pages/ServicesPage';
import { ContractsPage } from './pages/ContractsPage';
import { SlaPage } from './pages/SlaPage';
import { TaxesPage } from './pages/TaxesPage';
import { UsersPage } from './pages/UsersPage';
import { SettingsPage } from './pages/SettingsPage';
import { AuditPage } from './pages/AuditPage';
import { AccountPage } from './pages/AccountPage';

// APEX Billing — the app shell: sign-in, then a sidebar and the page for
// the address (#/clients, #/users, ...), so the browser's back button and
// bookmarks work.

const PAGES = [
  { path: 'home', label: 'Dashboard', icon: LayoutDashboard, el: HomePage },
  { path: 'clients', label: 'Clients', icon: Building2, el: ClientsPage, perm: 'view_billing' },
  { path: 'contracts', label: 'Contracts', icon: Handshake, el: ContractsPage, perm: 'view_billing' },
  { path: 'invoices', label: 'Invoices', icon: FileText, el: InvoicesPage, perm: 'view_billing' },
  { path: 'payments', label: 'Payments', icon: Wallet, el: PaymentsPage, perm: 'view_billing' },
  { path: 'reports', label: 'Reports', icon: BarChart3, el: ReportsPage, perm: 'view_reports' },
  { path: 'services', label: 'Services', icon: Network, el: ServicesPage, perm: 'view_billing' },
  { path: 'products', label: 'Products', icon: Package, el: ProductsPage, perm: 'view_billing' },
  { path: 'taxes', label: 'Taxes', icon: Percent, el: TaxesPage, perm: 'view_billing' },
  { path: 'slas', label: 'SLAs', icon: ShieldCheck, el: SlaPage, perm: 'view_billing' },
  { path: 'users', label: 'Users', icon: Users, el: UsersPage, perm: 'manage_users' },
  { path: 'settings', label: 'Company settings', icon: Settings, el: SettingsPage, perm: 'manage_settings' },
  { path: 'audit', label: 'Audit log', icon: ScrollText, el: AuditPage, perm: 'view_audit' },
  { path: 'account', label: 'My account', icon: UserCircle, el: AccountPage },
];

// #/services/12?x=1 → { path: 'services', arg: '12', query: URLSearchParams }
const readRoute = () => {
  const [main, search = ''] = window.location.hash.replace(/^#\/?/, '').split('?');
  const [path, ...rest] = (main || 'home').split('/');
  return { path, arg: rest.join('/'), query: new URLSearchParams(search) };
};

const useTheme = () => {
  const [dark, setDark] = useState(() => {
    try { return localStorage.getItem('apex-billing-theme') === 'dark'; } catch { return false; }
  });
  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark);
    try { localStorage.setItem('apex-billing-theme', dark ? 'dark' : 'light'); } catch { /* ignore */ }
  }, [dark]);
  return [dark, setDark];
};

export default function App() {
  const [user, setUser] = useState(null);
  const [checking, setChecking] = useState(true);
  const [notice, setNotice] = useState('');
  const [route, setRoute] = useState(readRoute);
  const [dark, setDark] = useTheme();
  const [menuOpen, setMenuOpen] = useState(false);

  useEffect(() => {
    const onHash = () => { setRoute(readRoute()); setMenuOpen(false); };
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  // Signing out ends the session on the server too. A session that ended
  // on its own (idle, signed out elsewhere) just shows the sign-in page.
  const signOut = useCallback((msg = '', ended = false) => {
    if (!ended) api('/api/auth/logout', { method: 'POST', body: {} }).catch(() => {});
    setUser(null);
    setNotice(msg);
  }, []);
  useEffect(() => { setSessionEndedHandler((msg) => signOut(msg, true)); }, [signOut]);
  // The server says a password change / two-step setup comes first.
  useEffect(() => { setGateHandler(() => { api('/api/me').then(d => setUser(d.user)).catch(() => {}); }); }, []);

  // Already signed in (the session cookie)?
  useEffect(() => {
    api('/api/me')
      .then(d => setUser(d.user))
      .catch(() => {})
      .finally(() => setChecking(false));
  }, []);

  const can = useCallback((perm) => !!user?.permissions?.includes(perm), [user]);
  const ctx = { user, setUser, can, signOut };

  if (checking) return <div className="h-full flex items-center justify-center"><Spinner label="Starting…" /></div>;
  if (!user) {
    return <LoginPage notice={notice} onSignedIn={(u) => { setNotice(''); setUser(u); }} />;
  }
  if (user.must_change_password) {
    return <AuthContext.Provider value={ctx}><ChangePasswordPage forced /></AuthContext.Provider>;
  }
  if (user.mfa_setup_required) {
    return <AuthContext.Provider value={ctx}><TwoStepSetup forced /></AuthContext.Provider>;
  }

  // The printed invoice: full page, no sidebar.
  if (route.path === 'print' && can('view_billing') && /^\d+$/.test(route.arg)) {
    return <AuthContext.Provider value={ctx}><InvoicePrint id={route.arg} /></AuthContext.Provider>;
  }
  if (route.path === 'statement' && can('view_billing') && /^\d+$/.test(route.arg)) {
    return <AuthContext.Provider value={ctx}><StatementPrint id={route.arg} query={route.query} /></AuthContext.Provider>;
  }

  const allowed = PAGES.filter(p => !p.perm || can(p.perm));
  const page = allowed.find(p => p.path === route.path) || allowed[0];
  const Page = page.el;

  return (
    <AuthContext.Provider value={ctx}>
      <div className="h-full flex">
        {/* Sidebar (a drawer on small screens) */}
        <aside className={`fixed md:static z-40 inset-y-0 left-0 w-60 shrink-0 flex flex-col bg-white dark:bg-zinc-950 border-r border-zinc-200 dark:border-zinc-800 transition-transform ${menuOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'}`}>
          <div className="h-14 flex items-center gap-2.5 px-4 border-b border-zinc-200 dark:border-zinc-800">
            <div className="w-8 h-8 rounded-lg bg-indigo-600 flex items-center justify-center text-white"><Receipt className="w-4 h-4" /></div>
            <div className="min-w-0">
              <div className="text-sm font-bold text-zinc-900 dark:text-white">APEX Billing</div>
              <div className="text-[10px] uppercase tracking-wider text-zinc-500">Clients · Invoices · Payments</div>
            </div>
            <button type="button" className="ml-auto md:hidden p-1 cursor-pointer" onClick={() => setMenuOpen(false)} aria-label="Close menu"><X className="w-4 h-4" /></button>
          </div>
          <nav className="flex-1 overflow-y-auto p-2 space-y-0.5">
            {allowed.map(p => {
              const Icon = p.icon;
              const active = p.path === page.path;
              return (
                <a key={p.path} href={`#/${p.path}`}
                  className={`flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm font-medium ${active
                    ? 'bg-indigo-600 text-white' : 'text-zinc-700 dark:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-900'}`}>
                  <Icon className="w-4 h-4" /> {p.label}
                </a>
              );
            })}
          </nav>
          <div className="p-3 border-t border-zinc-200 dark:border-zinc-800">
            <a href="#/account" title="My account" className="flex items-center gap-2.5 -m-1 p-1 rounded-lg hover:bg-zinc-100 dark:hover:bg-zinc-900">
              <Avatar user={user} size="md" />
              <div className="min-w-0">
                <div className="text-sm font-semibold text-zinc-900 dark:text-zinc-100 truncate">{user.name}</div>
                <div className="text-xs text-zinc-500 truncate">{user.role_label} · {user.email}</div>
              </div>
            </a>
            <div className="mt-2 flex gap-1">
              <button type="button" onClick={() => setDark(d => !d)} title={dark ? 'Light mode' : 'Dark mode'}
                className="p-2 rounded-lg text-zinc-600 dark:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-900 cursor-pointer">
                {dark ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
              </button>
              <button type="button" onClick={() => signOut('')}
                className="flex-1 inline-flex items-center justify-center gap-1.5 px-3 py-2 rounded-lg text-sm text-zinc-700 dark:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-900 cursor-pointer">
                <LogOut className="w-4 h-4" /> Sign out
              </button>
            </div>
          </div>
        </aside>
        {menuOpen && <div className="fixed inset-0 z-30 bg-black/40 md:hidden" onClick={() => setMenuOpen(false)} />}

        <main className="flex-1 min-w-0 overflow-y-auto">
          <div className="md:hidden h-12 flex items-center gap-2 px-3 border-b border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-950">
            <button type="button" onClick={() => setMenuOpen(true)} className="p-1.5 cursor-pointer" aria-label="Open menu"><Menu className="w-5 h-5" /></button>
            <span className="font-semibold text-sm">APEX Billing</span>
          </div>
          <div className="p-4 md:p-6 max-w-7xl mx-auto">
            <Page arg={route.arg} query={route.query} />
          </div>
        </main>
      </div>
    </AuthContext.Provider>
  );
}
