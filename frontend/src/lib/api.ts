import axios, { AxiosError, type AxiosInstance, type InternalAxiosRequestConfig } from 'axios';

export const TOKEN_STORAGE_KEY = 'pv_token';

export const api: AxiosInstance = axios.create({
  baseURL: '/api',
  withCredentials: true
});

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_STORAGE_KEY);
  } catch {
    // Private browsing modes can throw on storage access.
    return null;
  }
}

export function setToken(token: string | null) {
  try {
    if (token) localStorage.setItem(TOKEN_STORAGE_KEY, token);
    else localStorage.removeItem(TOKEN_STORAGE_KEY);
  } catch {
    /* storage unavailable; the session cookie still carries the request */
  }
}

api.interceptors.request.use((config) => {
  const token = getToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

/* -----------------------------------------------------------------------------
 * Silent token refresh
 *
 * Access tokens are short-lived by design, so an expired one is an ordinary
 * event rather than an error. On the first 401 the client exchanges its
 * refresh cookie for a new access token and replays the original request; only
 * if that fails does the session actually end.
 *
 * Concurrent 401s share a single refresh: without the shared promise, a page
 * that fires six requests at once would trigger six refreshes, and because the
 * server rotates the refresh token on every use, five of them would present an
 * already-consumed token and sign the user out.
 * -------------------------------------------------------------------------- */

type RetriableRequest = InternalAxiosRequestConfig & { _retried?: boolean };

let refreshInFlight: Promise<string | null> | null = null;

async function refreshAccessToken(): Promise<string | null> {
  try {
    // Bare axios, not `api`: the instance's interceptors would recurse.
    const response = await axios.post<{ token: string }>('/api/auth/refresh', null, {
      withCredentials: true
    });
    const token = response.data?.token ?? null;
    setToken(token);
    return token;
  } catch {
    setToken(null);
    return null;
  }
}

/** Called when the session is definitively over. Set by the auth layer. */
let onSessionExpired: (() => void) | null = null;

export function setSessionExpiredHandler(handler: () => void) {
  onSessionExpired = handler;
}

api.interceptors.response.use(
  (response) => response,
  async (error: AxiosError) => {
    const original = error.config as RetriableRequest | undefined;

    const isAuthEndpoint =
      original?.url?.includes('/auth/login') ||
      original?.url?.includes('/auth/refresh') ||
      original?.url?.includes('/auth/logout');

    if (error.response?.status === 401 && original && !original._retried && !isAuthEndpoint) {
      original._retried = true;

      refreshInFlight = refreshInFlight ?? refreshAccessToken().finally(() => {
        refreshInFlight = null;
      });
      const token = await refreshInFlight;

      if (token) {
        original.headers.Authorization = `Bearer ${token}`;
        return api(original);
      }

      onSessionExpired?.();
    }

    return Promise.reject(error);
  }
);

/**
 * Turns an axios failure into a message worth showing a person.
 *
 * The server's own message is preferred when there is one — it is written for
 * this situation — and network failures are named as such rather than
 * surfacing as a bare "Request failed".
 */
export function errorMessage(error: unknown, fallback: string): string {
  if (axios.isAxiosError(error)) {
    const serverMessage = (error.response?.data as { message?: string } | undefined)?.message;
    if (serverMessage) return serverMessage;
    if (error.code === 'ERR_NETWORK') return fallback;
  }
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

export function statusOf(error: unknown): number | undefined {
  return axios.isAxiosError(error) ? error.response?.status : undefined;
}
