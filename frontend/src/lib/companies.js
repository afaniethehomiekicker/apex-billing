import { useEffect, useState } from 'react';
import { api } from './api';

// Our companies (the ones that bill), loaded once and shared by every page.
// Call reloadCompanies() after adding or changing one.

let cache = null;
let pending = null;
const listeners = new Set();

export const reloadCompanies = () => {
  pending = api('/api/companies?status=all').then(d => {
    cache = d.companies || [];
    listeners.forEach(fn => fn(cache));
    return cache;
  }).finally(() => { pending = null; });
  return pending;
};

const loadCompanies = () => (cache ? Promise.resolve(cache) : (pending || reloadCompanies()));

// All companies (active and inactive), default first. [] until loaded.
export const useCompanies = () => {
  const [list, setList] = useState(cache || []);
  useEffect(() => {
    listeners.add(setList);
    loadCompanies().then(setList).catch(() => {});
    return () => { listeners.delete(setList); };
  }, []);
  return list;
};

export const defaultCompany = (list) => list.find(c => c.is_default) || list[0] || null;

export const companyName = (list, id) => list.find(c => c.id === Number(id))?.name || '';
