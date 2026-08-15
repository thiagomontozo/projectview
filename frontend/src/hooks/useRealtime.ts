import { useCallback, useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { getToken } from '../lib/api';
import { keys } from '../lib/queries';
import type { RealtimeMessage } from '../types';

/**
 * WebSocket client.
 *
 * Anything that persists still goes through REST — that is where validation,
 * authorization and the audit trail live. The socket carries two things REST
 * would be wrong for: server-pushed invalidations, and *ephemeral* state
 * (presence, typing) that is obsolete within seconds and costs nothing to lose
 * on a reconnect.
 */
class RealtimeClient {
  private socket: WebSocket | null = null;
  private token: string | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private attempts = 0;
  private listeners = new Set<(message: RealtimeMessage) => void>();
  /** Frames requested before the socket opened, replayed once it does. */
  private queue: string[] = [];

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
      this.queue.splice(0).forEach((frame) => socket.send(frame));
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
      // Exponential backoff, capped: a server restart must not turn into a
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

  send(payload: Record<string, unknown>) {
    const frame = JSON.stringify(payload);
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(frame);
    } else {
      // Typing frames are worthless once stale, so the queue is capped hard.
      if (this.queue.length < 4) this.queue.push(frame);
    }
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

/**
 * Connects the socket and turns pushed events into cache invalidations.
 *
 * Invalidating rather than merging pushed payloads into the cache keeps one
 * source of truth for the shape of the data: the query that already knows how
 * to fetch it. The alternative is two code paths that drift.
 */
export function useRealtime() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const token = getToken();
    if (!token) return;

    client.connect(token);

    return client.subscribe((message) => {
      switch (message.type) {
        case 'notification':
          void queryClient.invalidateQueries({ queryKey: keys.notifications });
          break;
        case 'chat:message': {
          const payload = message.payload as { channel?: string; parentId?: string } | undefined;
          void queryClient.invalidateQueries({ queryKey: keys.channels });
          if (payload?.channel) {
            void queryClient.invalidateQueries({ queryKey: keys.messages(payload.channel) });
          }
          if (payload?.parentId) {
            void queryClient.invalidateQueries({ queryKey: keys.thread(payload.parentId) });
          }
          break;
        }
        case 'chat:reaction': {
          const payload = message.payload as { messageId?: string } | undefined;
          void queryClient.invalidateQueries({ queryKey: keys.channels });
          void queryClient.invalidateQueries({ queryKey: ['chat'] });
          void payload;
          break;
        }
        case 'presence':
          void queryClient.invalidateQueries({ queryKey: keys.presence });
          break;
        default:
          break;
      }
    });
  }, [queryClient]);
}

interface TypingState {
  /** Names currently typing in the watched channel. */
  names: string[];
  /** Call on every keystroke; throttled internally. */
  notifyTyping: () => void;
  stopTyping: () => void;
}

/**
 * Typing indicators for one channel.
 *
 * Outbound frames are throttled to one every three seconds: the server treats
 * a repeat as a keep-alive and only broadcasts transitions, so sending one per
 * keystroke would be pure waste on both ends.
 */
export function useTyping(channelId: string | undefined, selfId: string | undefined): TypingState {
  const [typing, setTyping] = useState<Map<string, string>>(new Map());
  const lastSent = useRef(0);
  const stopTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    setTyping(new Map());
  }, [channelId]);

  useEffect(() => {
    return client.subscribe((message) => {
      if (message.type !== 'typing') return;
      const payload = message.payload as
        | { channel?: string; userId?: string; name?: string; typing?: boolean }
        | undefined;
      // Events arrive for every channel; the ones for other conversations are
      // ignored here rather than filtered server-side, since the hub does not
      // know channel membership.
      if (!payload?.userId || payload.channel !== channelId) return;
      if (payload.userId === selfId) return;

      setTyping((current) => {
        const next = new Map(current);
        if (payload.typing) next.set(payload.userId!, payload.name || payload.userId!);
        else next.delete(payload.userId!);
        return next;
      });
    });
  }, [channelId, selfId]);

  const notifyTyping = useCallback(() => {
    if (!channelId) return;
    const now = Date.now();
    if (now - lastSent.current > 3000) {
      lastSent.current = now;
      client.send({ type: 'typing', channel: channelId });
    }
    // Also stop explicitly after a pause, rather than waiting for the
    // server-side expiry — it looks immediate to everyone else.
    if (stopTimer.current) clearTimeout(stopTimer.current);
    stopTimer.current = setTimeout(() => {
      lastSent.current = 0;
      client.send({ type: 'typing:stop', channel: channelId });
    }, 4000);
  }, [channelId]);

  const stopTyping = useCallback(() => {
    if (!channelId) return;
    if (stopTimer.current) clearTimeout(stopTimer.current);
    lastSent.current = 0;
    client.send({ type: 'typing:stop', channel: channelId });
  }, [channelId]);

  return { names: [...typing.values()], notifyTyping, stopTyping };
}

export function disconnectRealtime() {
  client.disconnect();
}
