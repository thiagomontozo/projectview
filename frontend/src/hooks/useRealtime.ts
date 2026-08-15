import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { getToken } from '../lib/api';
import { keys } from '../lib/queries';
import type { RealtimeMessage } from '../types';

/**
 * Push-only WebSocket client.
 *
 * The server never expects app data from the client — every write goes through
 * REST — so this connection exists purely to be told that something changed.
 * Rather than merging pushed payloads into the cache by hand, it invalidates
 * the affected queries and lets the data layer refetch: one source of truth
 * for the shape of the data, and no chance of the two drifting apart.
 */
class RealtimeClient {
  private socket: WebSocket | null = null;
  private token: string | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private attempts = 0;
  private listeners = new Set<(message: RealtimeMessage) => void>();

  connect(token: string) {
    if (this.socket && this.token === token && this.socket.readyState <= WebSocket.OPEN) return;
    this.token = token;
    this.open();
  }

  private open() {
    if (!this.token) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const socket = new WebSocket(
      `${protocol}//${window.location.host}/ws?token=${encodeURIComponent(this.token)}`
    );

    socket.onopen = () => {
      this.attempts = 0;
    };

    socket.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as RealtimeMessage;
        this.listeners.forEach((listener) => listener(message));
      } catch {
        // A frame we cannot parse is not worth tearing the connection down for.
      }
    };

    socket.onclose = () => {
      this.socket = null;
      if (!this.token || this.reconnectTimer) return;
      // Exponential backoff, capped: a server restart should not turn into a
      // reconnect storm from every open tab.
      const delay = Math.min(30_000, 1000 * 2 ** this.attempts++);
      this.reconnectTimer = setTimeout(() => {
        this.reconnectTimer = null;
        this.open();
      }, delay);
    };

    socket.onerror = () => socket.close();
    this.socket = socket;
  }

  subscribe(listener: (message: RealtimeMessage) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  disconnect() {
    this.token = null;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.reconnectTimer = null;
    this.socket?.close();
    this.socket = null;
  }
}

const client = new RealtimeClient();

export function useRealtime() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const token = getToken();
    if (!token) return;

    client.connect(token);

    return client.subscribe((message) => {
      if (message.type === 'notification') {
        void queryClient.invalidateQueries({ queryKey: keys.notifications });
      }
      if (message.type === 'chat:message') {
        const payload = message.payload as { channel?: string } | undefined;
        void queryClient.invalidateQueries({ queryKey: keys.channels });
        if (payload?.channel) {
          void queryClient.invalidateQueries({ queryKey: keys.messages(payload.channel) });
        }
      }
    });
  }, [queryClient]);
}

export function disconnectRealtime() {
  client.disconnect();
}
