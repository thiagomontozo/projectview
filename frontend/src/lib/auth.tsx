import { useQueryClient } from '@tanstack/react-query';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode
} from 'react';
import { api, setSessionExpiredHandler, setToken } from './api';
import { keys, useMe } from './queries';
import type { User } from '../types';

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  adEnabled: boolean;
  signIn: (username: string, password: string, mode: 'ad' | 'local') => Promise<void>;
  signOut: () => Promise<void>;
  /** Set when a session ends on its own, so the login screen can explain why. */
  expiredNotice: boolean;
  clearExpiredNotice: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [adEnabled, setAdEnabled] = useState(false);
  const [expiredNotice, setExpiredNotice] = useState(false);

  const { data, isLoading, isError } = useMe();
  const user = isError ? null : (data?.user ?? null);

  useEffect(() => {
    // Which login modes to offer is public information, needed before anyone
    // has authenticated.
    api
      .get<{ adEnabled: boolean }>('/auth/config')
      .then((response) => setAdEnabled(response.data.adEnabled))
      .catch(() => setAdEnabled(false));
  }, []);

  // The api layer calls this once a refresh has definitively failed. Clearing
  // the cache here is what stops a stale user object from keeping the shell on
  // screen after the session is gone.
  useEffect(() => {
    setSessionExpiredHandler(() => {
      setExpiredNotice(true);
      queryClient.clear();
    });
  }, [queryClient]);

  const signIn = useCallback(
    async (username: string, password: string, mode: 'ad' | 'local') => {
      const response = await api.post<{ token: string; user: User }>('/auth/login', {
        username,
        password,
        mode
      });
      setToken(response.data.token);
      setExpiredNotice(false);
      // Seed the cache so the shell renders immediately instead of flashing a
      // loading state for data we already hold.
      queryClient.setQueryData(keys.me, { user: response.data.user });
      await queryClient.invalidateQueries();
    },
    [queryClient]
  );

  const signOut = useCallback(async () => {
    try {
      await api.post('/auth/logout');
    } catch {
      // Signing out must succeed locally even if the request fails, otherwise
      // a user on a broken connection is stuck signed in.
    }
    setToken(null);
    queryClient.clear();
  }, [queryClient]);

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      loading: isLoading,
      adEnabled,
      signIn,
      signOut,
      expiredNotice,
      clearExpiredNotice: () => setExpiredNotice(false)
    }),
    [user, isLoading, adEnabled, signIn, signOut, expiredNotice]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth must be used inside an AuthProvider');
  return context;
}
