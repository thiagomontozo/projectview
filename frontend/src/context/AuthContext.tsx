import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react';
import api from '../api/client';
import type { User } from '../types';

interface AuthContextValue {
  user: User | null;
  setUser: (u: User | null) => void;
  loading: boolean;
  login: (username: string, password: string, mode: 'ad' | 'local') => Promise<User>;
  logout: () => Promise<void>;
  adEnabled: boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [adEnabled, setAdEnabled] = useState(false);

  useEffect(() => {
    api
      .get('/auth/config')
      .then((res) => setAdEnabled(res.data.adEnabled))
      .catch(() => {});

    const token = localStorage.getItem('pv_token');
    if (!token) {
      setLoading(false);
      return;
    }
    api
      .get('/auth/me')
      .then((res) => setUser(res.data.user))
      .catch(() => localStorage.removeItem('pv_token'))
      .finally(() => setLoading(false));
  }, []);

  const login = useCallback(async (username: string, password: string, mode: 'ad' | 'local') => {
    const res = await api.post('/auth/login', { username, password, mode });
    localStorage.setItem('pv_token', res.data.token);
    setUser(res.data.user);
    return res.data.user as User;
  }, []);

  const logout = useCallback(async () => {
    await api.post('/auth/logout').catch(() => {});
    localStorage.removeItem('pv_token');
    setUser(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, setUser, loading, login, logout, adEnabled }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider');
  return ctx;
}
