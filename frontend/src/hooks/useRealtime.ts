import { useEffect, useRef, useState } from 'react';
import { useAuth } from '../context/AuthContext';
import type { RealtimeMessage } from '../types';

type Listener = (msg: RealtimeMessage) => void;

/**
 * Thin wrapper around the backend's push-only WebSocket ("/ws?token=...").
 * Unlike the previous Socket.IO design, clients never *send* app data over
 * this socket - all writes go through the REST API, and the server just
 * pushes "notification" / "chat:message" events to open connections. This
 * hook owns a single shared connection and lets components subscribe to
 * messages via `subscribe`.
 */
class RealtimeClient {
  private ws: WebSocket | null = null;
  private listeners = new Set<Listener>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private token: string | null = null;

  connect(token: string) {
    if (this.ws && this.token === token && this.ws.readyState <= WebSocket.OPEN) return;
    this.token = token;
    this.open();
  }

  private open() {
    if (!this.token) return;
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${proto}//${window.location.host}/ws?token=${encodeURIComponent(this.token)}`;
    const socket = new WebSocket(url);

    socket.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data) as RealtimeMessage;
        this.listeners.forEach((l) => l(msg));
      } catch {
        // ignore malformed frames
      }
    };

    socket.onclose = () => {
      this.ws = null;
      if (this.token && !this.reconnectTimer) {
        this.reconnectTimer = setTimeout(() => {
          this.reconnectTimer = null;
          this.open();
        }, 2000);
      }
    };

    socket.onerror = () => socket.close();

    this.ws = socket;
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  disconnect() {
    this.token = null;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.ws?.close();
    this.ws = null;
  }
}

const client = new RealtimeClient();

export function useRealtime(onMessage?: Listener) {
  const { user } = useAuth();
  const handlerRef = useRef(onMessage);
  handlerRef.current = onMessage;

  useEffect(() => {
    const token = localStorage.getItem('pv_token');
    if (!user || !token) return undefined;
    client.connect(token);
    return undefined;
  }, [user]);

  useEffect(() => {
    if (!handlerRef.current) return undefined;
    return client.subscribe((msg) => handlerRef.current?.(msg));
  }, []);
}

export function disconnectRealtime() {
  client.disconnect();
}
