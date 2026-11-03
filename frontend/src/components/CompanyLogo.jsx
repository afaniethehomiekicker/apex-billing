import React, { useEffect, useState } from 'react';
import { apiBlobURL } from '../lib/api';

// A logo picture from the server (signed-in only, so it is fetched and
// shown from memory). src: e.g. /api/companies/3/logo. version: anything
// that changes when the logo does. onReady(true|false) once it has loaded
// or failed (the printed invoice waits for it).
export const CompanyLogo = ({ src, version, className = '', alt = 'Logo', onReady }) => {
  const [url, setUrl] = useState('');
  useEffect(() => {
    if (!src) { onReady?.(false); return undefined; }
    let u = '';
    let live = true;
    apiBlobURL(src).then(x => {
      u = x;
      if (live) setUrl(x); else URL.revokeObjectURL(x);
    }).catch(() => { if (live) { setUrl(''); onReady?.(false); } });
    return () => { live = false; if (u) URL.revokeObjectURL(u); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [src, version]);
  if (!url) return null;
  return <img src={url} alt={alt} className={className} onLoad={() => onReady?.(true)} onError={() => onReady?.(false)} />;
};
