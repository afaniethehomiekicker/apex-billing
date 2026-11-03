import React, { useEffect, useRef, useState } from 'react';
import { X, Loader2, ChevronLeft, ChevronRight, Eye, EyeOff } from 'lucide-react';

// Small shared building blocks, so every page looks and behaves the same.

export const inputCls = 'w-full px-3 py-2 rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-sm text-zinc-900 dark:text-zinc-100 placeholder:text-zinc-400 focus:ring-2 focus:ring-indigo-500 focus:outline-hidden disabled:opacity-60';

export const Field = ({ label, required, hint, children, className = '' }) => (
  <label className={`block ${className}`}>
    <span className="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1">
      {label}{required && <span className="text-rose-500"> *</span>}
    </span>
    {children}
    {hint && <span className="block mt-1 text-[11px] text-zinc-500 dark:text-zinc-400">{hint}</span>}
  </label>
);

export const Input = (props) => <input {...props} className={`${inputCls} ${props.className || ''}`} />;
// A password box with an eye button to show or hide what was typed.
// It always starts hidden, and goes back to hidden when the form is sent.
export const PasswordInput = ({ className = '', ...props }) => {
  const [show, setShow] = useState(false);
  const box = useRef(null);
  useEffect(() => {
    const form = box.current?.form;
    if (!form) return undefined;
    const hide = () => setShow(false);
    form.addEventListener('submit', hide);
    return () => form.removeEventListener('submit', hide);
  }, []);
  return (
    <span className="relative block">
      <input {...props} ref={box} type={show ? 'text' : 'password'} className={`${inputCls} pr-10 ${className}`}
        autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      <button type="button" onClick={() => setShow(v => !v)} disabled={props.disabled}
        aria-label={show ? 'Hide password' : 'Show password'} title={show ? 'Hide password' : 'Show password'} aria-pressed={show}
        className="absolute inset-y-0 right-0 flex items-center px-3 text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-100 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed">
        {show ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
      </button>
    </span>
  );
};
export const Textarea = (props) => <textarea rows={3} {...props} className={`${inputCls} resize-y ${props.className || ''}`} />;
export const Select = ({ children, ...props }) => <select {...props} className={`${inputCls} ${props.className || ''}`}>{children}</select>;

const BTN = {
  primary: 'bg-indigo-600 hover:bg-indigo-700 text-white',
  secondary: 'border border-zinc-300 dark:border-zinc-700 text-zinc-800 dark:text-zinc-100 hover:bg-zinc-100 dark:hover:bg-zinc-800',
  danger: 'bg-rose-600 hover:bg-rose-700 text-white',
  ghost: 'text-zinc-700 dark:text-zinc-300 hover:bg-zinc-200/70 dark:hover:bg-zinc-800',
};
export const Button = ({ variant = 'primary', busy, icon: Icon, children, className = '', ...props }) => (
  <button type="button" {...props} disabled={props.disabled || busy}
    className={`inline-flex items-center justify-center gap-1.5 px-3.5 py-2 rounded-lg text-sm font-semibold cursor-pointer disabled:opacity-60 disabled:cursor-not-allowed ${BTN[variant]} ${className}`}>
    {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : Icon ? <Icon className="w-4 h-4" /> : null}
    {children}
  </button>
);

export const Badge = ({ tone = 'zinc', children }) => {
  const tones = {
    zinc: 'bg-zinc-100 text-zinc-700 border-zinc-300 dark:bg-zinc-800 dark:text-zinc-300 dark:border-zinc-700',
    green: 'bg-emerald-50 text-emerald-700 border-emerald-300 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800',
    amber: 'bg-amber-50 text-amber-700 border-amber-300 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800',
    red: 'bg-rose-50 text-rose-700 border-rose-300 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800',
    indigo: 'bg-indigo-50 text-indigo-700 border-indigo-300 dark:bg-indigo-950/40 dark:text-indigo-300 dark:border-indigo-800',
  };
  return <span className={`inline-flex px-2 py-0.5 rounded-full border text-[11px] font-semibold whitespace-nowrap ${tones[tone] || tones.zinc}`}>{children}</span>;
};

export const ErrorText = ({ children }) => (children ? <p className="text-sm text-rose-600 dark:text-rose-400">{children}</p> : null);

export const Spinner = ({ label = 'Loading…' }) => (
  <div className="flex items-center gap-2 text-sm text-zinc-500 py-6"><Loader2 className="w-4 h-4 animate-spin" /> {label}</div>
);

export const PageHeader = ({ icon: Icon, title, subtitle, actions }) => (
  <div className="flex flex-wrap items-start justify-between gap-3 mb-5">
    <div>
      <h1 className="text-xl font-bold text-zinc-900 dark:text-zinc-100 flex items-center gap-2">
        {Icon && <Icon className="w-5 h-5 text-indigo-500" />} {title}
      </h1>
      {subtitle && <p className="text-sm text-zinc-500 dark:text-zinc-400 mt-0.5">{subtitle}</p>}
    </div>
    {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
  </div>
);

export const Card = ({ children, className = '' }) => (
  <div className={`rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 ${className}`}>{children}</div>
);

// A window over the page. Escape and the backdrop close it (onClose may
// refuse, e.g. to protect unsaved changes).
export const Modal = ({ title, onClose, children, footer, wide = false }) => {
  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose?.(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/50 p-4 overflow-y-auto" onMouseDown={onClose}>
      <div onMouseDown={(e) => e.stopPropagation()}
        className={`my-auto w-full ${wide ? 'max-w-3xl' : 'max-w-lg'} rounded-xl bg-white dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 shadow-xl`}>
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-zinc-200 dark:border-zinc-800">
          <h2 className="text-base font-semibold text-zinc-900 dark:text-zinc-100">{title}</h2>
          <button type="button" onClick={onClose} className="p-1 rounded text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800 cursor-pointer" aria-label="Close"><X className="w-4 h-4" /></button>
        </div>
        <div className="p-5">{children}</div>
        {footer && <div className="flex flex-wrap justify-end gap-2 px-5 py-3 border-t border-zinc-200 dark:border-zinc-800">{footer}</div>}
      </div>
    </div>
  );
};

export const Pager = ({ page, pageSize, total, onPage }) => {
  const pages = Math.max(1, Math.ceil((total || 0) / (pageSize || 50)));
  if (pages <= 1) return null;
  return (
    <div className="flex items-center justify-end gap-2 text-sm text-zinc-600 dark:text-zinc-400 mt-3">
      <button type="button" disabled={page <= 1} onClick={() => onPage(page - 1)} className="p-1 rounded hover:bg-zinc-200 dark:hover:bg-zinc-800 disabled:opacity-40 cursor-pointer" aria-label="Previous page"><ChevronLeft className="w-4 h-4" /></button>
      Page {page} of {pages}
      <button type="button" disabled={page >= pages} onClick={() => onPage(page + 1)} className="p-1 rounded hover:bg-zinc-200 dark:hover:bg-zinc-800 disabled:opacity-40 cursor-pointer" aria-label="Next page"><ChevronRight className="w-4 h-4" /></button>
    </div>
  );
};

// A table that scrolls sideways on small screens.
export const Table = ({ head, children }) => (
  <div className="overflow-x-auto rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900">
    <table className="w-full text-sm">
      <thead className="bg-zinc-50 dark:bg-zinc-800/60 text-zinc-600 dark:text-zinc-300 text-xs">
        <tr>{head.map((h, i) => (
          <th key={i} className={`px-3 py-2 font-semibold whitespace-nowrap ${h?.right ? 'text-right' : 'text-left'}`}>{h?.label ?? h}</th>
        ))}</tr>
      </thead>
      <tbody>{children}</tbody>
    </table>
  </div>
);
