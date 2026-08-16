import axios, { AxiosError, type AxiosInstance, type InternalAxiosRequestConfig } from 'axios';

export const TOKEN_STORAGE_KEY = 'pv_token';

export const api: AxiosInstance = axios.create({
  baseURL: '/api',
  withCredentials: true,
  /**
   * Repeat a parameter rather than bracket it: `?status=todo&status=done`, not
   * `?status[]=todo`.
   *
   * Axios brackets array parameters by default, and Go's net/http reads the
   * literal key — so `status[]` arrives as a parameter nothing looks at, and
   * every array filter is silently dropped. The board went out with each of its
   * five columns showing the same unfiltered page because of exactly this: no
   * error, no warning, just a filter that was never applied.
   */
  paramsSerializer: { indexes: null }
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

/**
 * Whether a failed refresh means a session actually ended.
 *
 * Only when the client believed it had one. A visitor who is simply not signed
 * in also gets a 401 from /auth/me and also fails to refresh — but for them
 * that is the normal state, not an expiry, and treating it as one is a loop:
 * the handler clears the query cache, the cleared `me` query immediately
 * refetches, gets another 401, and clears the cache again. The `me` query never
 * settles, `loading` never becomes false, and the login screen never renders.
 */
export function endsSession(hadToken: boolean, refreshedToken: string | null): boolean {
  return hadToken && refreshedToken === null;
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
      // Read before the refresh attempt clears it.
      const hadToken = getToken() !== null;

      refreshInFlight = refreshInFlight ?? refreshAccessToken().finally(() => {
        refreshInFlight = null;
      });
      const token = await refreshInFlight;

      if (token) {
        original.headers.Authorization = `Bearer ${token}`;
        return api(original);
      }

      if (endsSession(hadToken, token)) {
        onSessionExpired?.();
      }
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

/**
 * Starts a file download from the API.
 *
 * A plain navigation rather than a fetch-and-blob: the response carries
 * Content-Disposition, so the browser saves it and stays on the page, and the
 * session cookie authenticates it. Building a blob would mean holding the
 * whole export in memory to achieve exactly the same result.
 */
export function downloadUrl(path: string) {
  window.location.assign(`/api${path}`);
}
