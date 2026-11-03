import React from 'react';
import { Select } from './ui';

// Choose one of our companies. Inactive ones only show when already chosen.
// allLabel: adds an "all companies" choice (value '') for filters.
export const CompanySelect = ({ companies, value, onChange, allLabel, disabled, className = '' }) => {
  const list = (companies || []).filter(c => c.active || String(c.id) === String(value));
  return (
    <Select value={value ?? ''} disabled={disabled} className={className} onChange={(e) => onChange(e.target.value)}>
      {allLabel && <option value="">{allLabel}</option>}
      {list.map(c => (
        <option key={c.id} value={c.id}>{c.name}{c.is_default ? ' (default)' : ''}{!c.active ? ' (inactive)' : ''}</option>
      ))}
    </Select>
  );
};
