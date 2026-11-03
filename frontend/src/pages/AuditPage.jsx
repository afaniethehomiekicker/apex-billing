import React, { useEffect, useState } from 'react';
import { ScrollText, Search } from 'lucide-react';
import { api, qs } from '../lib/api';
import { fmtDateTime } from '../lib/format';
import { PageHeader, Select, Table, Pager, Spinner, ErrorText, inputCls } from '../components/ui';

// Who did what, when (admins).
export const AuditPage = () => {
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  const [type, setType] = useState('');
  const [page, setPage] = useState(1);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  useEffect(() => { const t = setTimeout(() => setDebounced(q.trim()), 300); return () => clearTimeout(t); }, [q]);
  useEffect(() => { setPage(1); }, [debounced, type]);
  useEffect(() => {
    api(`/api/audit${qs({ q: debounced, entity_type: type, page })}`).then(setData).catch(err => setError(err.message));
  }, [debounced, type, page]);

  return (
    <>
      <PageHeader icon={ScrollText} title="Audit log" subtitle="Every sign-in and change, newest first." />
      <div className="flex flex-wrap gap-2 mb-3">
        <span className="relative flex-1 min-w-[14rem] max-w-md">
          <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search details or person…" className={`${inputCls} pl-8`} />
        </span>
        <Select value={type} onChange={(e) => setType(e.target.value)} className="!w-auto">
          <option value="">Everything</option>
          <option value="client">Clients</option>
          <option value="contract">Contracts</option>
          <option value="subscription">Services</option>
          <option value="sla_plan">SLAs</option>
          <option value="user">Users & sign-ins</option>
          <option value="settings">Settings</option>
        </Select>
      </div>
      <ErrorText>{error}</ErrorText>
      {!data ? <Spinner /> : (
        <>
          <Table head={['When', 'Who', 'What', 'Details', 'From']}>
            {data.entries.length === 0 && <tr><td colSpan={5} className="px-3 py-6 text-center text-zinc-500">Nothing yet.</td></tr>}
            {data.entries.map(e => (
              <tr key={e.id} className="border-t border-zinc-100 dark:border-zinc-800 align-top">
                <td className="px-3 py-2 whitespace-nowrap text-zinc-600 dark:text-zinc-400">{fmtDateTime(e.created_at)}</td>
                <td className="px-3 py-2 whitespace-nowrap">{e.user_name || '—'}</td>
                <td className="px-3 py-2 whitespace-nowrap">{e.entity_type} · {e.action}</td>
                <td className="px-3 py-2 break-words">{e.details}</td>
                <td className="px-3 py-2 whitespace-nowrap font-mono text-xs text-zinc-500">{e.ip}</td>
              </tr>
            ))}
          </Table>
          <Pager page={data.page} pageSize={data.page_size} total={data.total} onPage={setPage} />
        </>
      )}
    </>
  );
};
