import { useEffect, useRef, useState } from 'react';
import api from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useRealtime } from '../hooks/useRealtime';
import type { ChatChannel, ChatMessage, PublicUser, RealtimeMessage } from '../types';

function initials(name = ''): string {
  return name
    .split(' ')
    .map((p) => p[0])
    .slice(0, 2)
    .join('')
    .toUpperCase();
}

export default function ChatPage() {
  const { user } = useAuth();
  const [channels, setChannels] = useState<ChatChannel[]>([]);
  const [activeChannel, setActiveChannel] = useState<ChatChannel | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [text, setText] = useState('');
  const [users, setUsers] = useState<PublicUser[]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);
  const activeChannelRef = useRef<ChatChannel | null>(null);
  activeChannelRef.current = activeChannel;

  useEffect(() => {
    api.get<ChatChannel[]>('/chat/channels').then((res) => {
      setChannels(res.data);
      setActiveChannel((current) => current || res.data[0] || null);
    });
    api.get<PublicUser[]>('/users').then((res) => setUsers(res.data));
  }, []);

  useEffect(() => {
    if (!activeChannel) return;
    api.get<ChatMessage[]>(`/chat/channels/${activeChannel.id}/messages`).then((res) => setMessages(res.data));
  }, [activeChannel]);

  // Messages are sent via REST (below) and received here in real time; the
  // backend's WebSocket is a push-only channel (see hooks/useRealtime.ts).
  // The server echoes a sender's own message back to them too (it pushes to
  // every channel member, sender included), so de-dupe by id since the
  // sender already appended it optimistically in send() below.
  useRealtime((msg: RealtimeMessage) => {
    if (msg.type === 'chat:message') {
      const message = msg.payload as ChatMessage;
      if (activeChannelRef.current && message.channel === activeChannelRef.current.id) {
        setMessages((prev) => (prev.some((m) => m.id === message.id) ? prev : [...prev, message]));
      }
    }
  });

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  async function send() {
    if (!text.trim() || !activeChannel) return;
    const body = text;
    setText('');
    const res = await api.post<ChatMessage>(`/chat/channels/${activeChannel.id}/messages`, { body });
    // The server also echoes this back over the WebSocket to other tabs;
    // append locally right away so the sender sees it instantly.
    setMessages((prev) => [...prev, res.data]);
  }

  async function startDM(otherUser: PublicUser) {
    const res = await api.post<ChatChannel>('/chat/channels', { type: 'dm', memberIds: [otherUser.id] });
    setChannels((prev) => {
      const exists = prev.find((c) => c.id === res.data.id);
      return exists ? prev : [res.data, ...prev];
    });
    setActiveChannel(res.data);
  }

  function channelLabel(c: ChatChannel): string {
    if (c.name) return c.name;
    if (c.type === 'dm') {
      const other = c.members.find((m) => m.id !== user?.id);
      return other ? other.name : 'Conversa';
    }
    return 'Canal';
  }

  return (
    <div>
      <div className="page-header">
        <h1>Chat Interno</h1>
      </div>
      <div className="card" style={{ display: 'grid', gridTemplateColumns: '260px 1fr', height: 'calc(100vh - 190px)', overflow: 'hidden' }}>
        <div style={{ borderRight: '1px solid var(--grid)', overflowY: 'auto', padding: 10 }}>
          <div className="label" style={{ padding: '4px 8px' }}>
            Canais
          </div>
          {channels.map((c) => (
            <div
              key={c.id}
              onClick={() => setActiveChannel(c)}
              style={{
                padding: '8px 10px',
                borderRadius: 8,
                fontSize: 13,
                cursor: 'pointer',
                background: activeChannel?.id === c.id ? '#eef4fc' : 'transparent',
                marginBottom: 2
              }}
            >
              {channelLabel(c)}
            </div>
          ))}

          <div className="label" style={{ padding: '14px 8px 4px' }}>
            Iniciar conversa
          </div>
          {users
            .filter((u) => u.id !== user?.id)
            .map((u) => (
              <div
                key={u.id}
                onClick={() => startDM(u)}
                style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', borderRadius: 8, cursor: 'pointer', fontSize: 13 }}
              >
                <div className="avatar" style={{ width: 20, height: 20, fontSize: 10, background: u.avatarColor || '#2a78d6' }}>
                  {initials(u.name)}
                </div>
                {u.name}
              </div>
            ))}
        </div>

        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '12px 16px', borderBottom: '1px solid var(--grid)', fontWeight: 600, fontSize: 14 }}>
            {activeChannel ? channelLabel(activeChannel) : 'Selecione um canal'}
          </div>
          <div style={{ flex: 1, overflowY: 'auto', padding: 16 }}>
            {messages.map((m) => (
              <div key={m.id} style={{ marginBottom: 12, display: 'flex', gap: 10 }}>
                <div className="avatar" style={{ background: m.author?.avatarColor || '#2a78d6' }}>
                  {initials(m.author?.name)}
                </div>
                <div>
                  <div style={{ fontSize: 13 }}>
                    <strong>{m.author?.name}</strong>{' '}
                    <span style={{ color: 'var(--text-secondary)', fontSize: 11 }}>
                      {new Date(m.createdAt).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' })}
                    </span>
                  </div>
                  <div style={{ fontSize: 14 }}>{m.body}</div>
                </div>
              </div>
            ))}
            <div ref={bottomRef} />
          </div>
          {activeChannel && (
            <div style={{ display: 'flex', gap: 8, padding: 12, borderTop: '1px solid var(--grid)' }}>
              <input
                className="input"
                placeholder="Escreva uma mensagem..."
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && send()}
              />
              <button className="btn btn-primary" onClick={send}>
                Enviar
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
