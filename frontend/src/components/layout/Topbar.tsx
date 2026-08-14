import { useEffect, useState, useRef } from 'react';
import { useAuth } from '../../context/AuthContext';
import { useRealtime } from '../../hooks/useRealtime';
import api from '../../api/client';
import type { AppNotification, RealtimeMessage } from '../../types';

function initials(name = ''): string {
  return name
    .split(' ')
    .map((p) => p[0])
    .slice(0, 2)
    .join('')
    .toUpperCase();
}

export default function Topbar() {
  const { user, logout } = useAuth();
  const [notifications, setNotifications] = useState<AppNotification[]>([]);
  const [open, setOpen] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    api
      .get<AppNotification[]>('/notifications')
      .then((res) => setNotifications(res.data))
      .catch(() => {});
  }, []);

  useRealtime((msg: RealtimeMessage) => {
    if (msg.type === 'notification') {
      setNotifications((prev) => [msg.payload as AppNotification, ...prev]);
    }
  });

  useEffect(() => {
    function onClickOutside(e: MouseEvent) {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener('mousedown', onClickOutside);
    return () => document.removeEventListener('mousedown', onClickOutside);
  }, []);

  const unread = notifications.filter((n) => !n.read).length;

  async function markAllRead() {
    await api.post('/notifications/read-all').catch(() => {});
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
  }

  return (
    <header
      style={{
        height: 58,
        borderBottom: '1px solid var(--border)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'flex-end',
        gap: 14,
        padding: '0 22px',
        background: 'var(--surface-1)'
      }}
    >
      <div ref={panelRef} style={{ position: 'relative' }}>
        <button className="btn btn-sm" onClick={() => setOpen((o) => !o)}>
          🔔 {unread > 0 && <span className="badge" style={{ background: '#d03b3b', color: '#fff' }}>{unread}</span>}
        </button>
        {open && (
          <div
            className="card"
            style={{
              position: 'absolute',
              right: 0,
              top: 40,
              width: 340,
              maxHeight: 420,
              overflowY: 'auto',
              padding: 10,
              zIndex: 40
            }}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', padding: '4px 6px 10px' }}>
              <strong style={{ fontSize: 13 }}>Notificações</strong>
              <button className="btn btn-sm" onClick={markAllRead}>
                Marcar todas como lidas
              </button>
            </div>
            {notifications.length === 0 && (
              <div style={{ padding: 12, color: 'var(--text-secondary)', fontSize: 13 }}>Nenhuma notificação.</div>
            )}
            {notifications.map((n) => (
              <div key={n.id} style={{ padding: '8px 6px', borderBottom: '1px solid var(--grid)', opacity: n.read ? 0.6 : 1 }}>
                <div style={{ fontSize: 13, fontWeight: 600 }}>{n.title}</div>
                <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{n.body}</div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <div className="avatar" style={{ background: user?.avatarColor || '#2a78d6' }}>
          {initials(user?.name)}
        </div>
        <div style={{ lineHeight: 1.2 }}>
          <div style={{ fontSize: 13, fontWeight: 600 }}>{user?.name}</div>
          <div style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{user?.role}</div>
        </div>
        <button className="btn btn-sm" onClick={logout}>
          Sair
        </button>
      </div>
    </header>
  );
}
