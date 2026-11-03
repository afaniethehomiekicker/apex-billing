import React, { useEffect, useRef, useState } from 'react';
import { Camera, Trash2 } from 'lucide-react';
import { api, apiBlobURL } from '../lib/api';
import { Button, ErrorText } from './ui';

// A user's profile picture (fetched while signed in, shown from memory), or
// their initials on a colour picked from their id when there is none.

const COLORS = ['bg-indigo-600', 'bg-emerald-600', 'bg-sky-600', 'bg-amber-600', 'bg-rose-600', 'bg-violet-600', 'bg-teal-600'];
const SIZES = { sm: 'w-8 h-8 text-xs', md: 'w-10 h-10 text-sm', lg: 'w-24 h-24 text-2xl' };

const initials = (name = '') => name.trim().split(/\s+/).slice(0, 2).map(w => w[0] || '').join('').toUpperCase() || '?';

export const Avatar = ({ user, size = 'sm', className = '' }) => {
  const [url, setUrl] = useState('');
  const has = !!user?.has_avatar;
  useEffect(() => {
    if (!has) { setUrl(''); return undefined; }
    let u = '';
    let live = true;
    apiBlobURL(`/api/users/${user.id}/avatar`).then(x => {
      u = x;
      if (live) setUrl(x); else URL.revokeObjectURL(x);
    }).catch(() => { if (live) setUrl(''); });
    return () => { live = false; if (u) URL.revokeObjectURL(u); };
  }, [user?.id, has, user?.avatar_version]);
  const box = `${SIZES[size] || SIZES.sm} shrink-0 rounded-full overflow-hidden ${className}`;
  if (url) return <img src={url} alt="" className={`${box} object-cover`} />;
  return (
    <div className={`${box} ${COLORS[(user?.id || 0) % COLORS.length]} text-white font-semibold flex items-center justify-center select-none`} aria-hidden="true">
      {initials(user?.name)}
    </div>
  );
};

// Picture with Change / Remove. base: '/api/me' for yourself, or
// '/api/users/12' for an admin changing someone else's.
export const AvatarEditor = ({ user, base, onChanged }) => {
  const input = useRef(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const upload = async (e) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) { setError('The picture is too big (max 2 MB)'); return; }
    setError('');
    setBusy('upload');
    try {
      const form = new FormData();
      form.append('file', file);
      const d = await api(`${base}/avatar`, { method: 'POST', form });
      onChanged(d.user);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy('');
    }
  };
  const remove = async () => {
    setError('');
    setBusy('remove');
    try {
      const d = await api(`${base}/avatar/remove`, { method: 'POST', body: {} });
      onChanged(d.user);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy('');
    }
  };
  return (
    <div className="flex items-center gap-4">
      <Avatar user={user} size="lg" />
      <div className="space-y-2">
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" icon={Camera} busy={busy === 'upload'} onClick={() => input.current?.click()}>
            {user.has_avatar ? 'Change picture' : 'Add picture'}
          </Button>
          {user.has_avatar && <Button variant="ghost" icon={Trash2} busy={busy === 'remove'} onClick={remove}>Remove</Button>}
        </div>
        <p className="text-[11px] text-zinc-500">PNG, JPG or WEBP, up to 2 MB. A square picture looks best.</p>
        <ErrorText>{error}</ErrorText>
      </div>
      <input ref={input} type="file" accept="image/png,image/jpeg,image/webp" className="hidden" onChange={upload} />
    </div>
  );
};
