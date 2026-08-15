/**
 * Queries for the enterprise surface: goals, portfolio and capacity,
 * baselines and earned value, the saved dashboard layout, machine credentials
 * and the privacy rights.
 *
 * Kept beside queries.ts rather than inside it: these are read by a handful of
 * screens an ordinary member never opens, and the split keeps the main data
 * layer navigable.
 */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './api';
import { get, keys } from './queries';
import type { User } from '../types';

/* --- Goals and OKRs ------------------------------------------------------------ */

export interface KeyResult {
  id: string;
  goalId: string;
  name: string;
  source: 'manual' | 'tasks_completed' | 'tasks_count';
  unit: string;
  startValue: number;
  targetValue: number;
  currentValue: number;
  projectId?: string;
  position: number;
  progress: number;
}

export interface Goal {
  id: string;
  name: string;
  description: string;
  spaceId?: string;
  teamId?: string;
  ownerId?: string;
  startDate?: string;
  dueDate?: string;
  status: 'active' | 'at_risk' | 'achieved' | 'missed';
  archived: boolean;
  keyResults: KeyResult[];
  progress: number;
}

export function useGoals(spaceId?: string) {
  return useQuery({
    queryKey: keys.goals(spaceId ?? 'all'),
    queryFn: () => get<Goal[]>('/goals', spaceId ? { spaceId } : undefined)
  });
}

export function useCreateGoal() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: Partial<Goal>) => api.post<Goal>('/goals', body).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: ['goals'] })
  });
}

export function useUpdateGoal() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string } & Partial<Goal>) =>
      api.put<Goal>(`/goals/${id}`, body).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: ['goals'] })
  });
}

export function useDeleteGoal() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/goals/${id}`),
    onSuccess: () => client.invalidateQueries({ queryKey: ['goals'] })
  });
}

export function useAddKeyResult() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ goalId, ...body }: { goalId: string } & Partial<KeyResult>) =>
      api.post<Goal>(`/goals/${goalId}/key-results`, body).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: ['goals'] })
  });
}

export function useSetKeyResultValue() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ goalId, keyResultId, value }: { goalId: string; keyResultId: string; value: number }) =>
      api.put<Goal>(`/goals/${goalId}/key-results/${keyResultId}`, { value }).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: ['goals'] })
  });
}

export function useDeleteKeyResult() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ goalId, keyResultId }: { goalId: string; keyResultId: string }) =>
      api.delete(`/goals/${goalId}/key-results/${keyResultId}`),
    onSuccess: () => client.invalidateQueries({ queryKey: ['goals'] })
  });
}

/* --- Portfolio and capacity ---------------------------------------------------- */

export interface PortfolioProject {
  id: string;
  name: string;
  key: string;
  color: string;
  status: string;
  ownerId?: string;
  startDate?: string;
  endDate?: string;
  totalTasks: number;
  doneTasks: number;
  overdueOpen: number;
  people: number;
  estimatedHours: number;
  trackedHours: number;
  progress: number;
  health: 'on_track' | 'at_risk' | 'off_track' | 'done';
}

export interface CapacityRow {
  userId: string;
  name: string;
  email: string;
  avatarColor: string;
  capacityHours: number;
  committedHours: number;
  projects: number;
  openTasks: number;
  utilisation: number;
}

export function usePortfolio(spaceId?: string) {
  return useQuery({
    queryKey: keys.portfolio(spaceId ?? 'all'),
    queryFn: () => get<PortfolioProject[]>('/portfolio', spaceId ? { spaceId } : undefined),
    retry: false
  });
}

export function useCapacity(weeks = 4) {
  return useQuery({
    queryKey: keys.capacity(String(weeks)),
    queryFn: () => {
      const from = new Date();
      const to = new Date(from.getTime() + weeks * 7 * 24 * 3600_000);
      return get<{ from: string; to: string; rows: CapacityRow[] }>('/portfolio/capacity', {
        from: from.toISOString(),
        to: to.toISOString()
      });
    },
    retry: false
  });
}

/* --- Baselines and earned value ------------------------------------------------ */

export interface Baseline {
  id: string;
  projectId: string;
  name: string;
  capturedAt: string;
}

export interface EarnedValue {
  asOf: string;
  bac: number;
  pv: number;
  ev: number;
  ac: number;
  sv: number;
  cv: number;
  /** Null rather than zero when the denominator is zero — see the server. */
  spi: number | null;
  cpi: number | null;
  eac: number | null;
  vac: number | null;
}

export function useBaselines(projectId: string | undefined) {
  return useQuery({
    queryKey: keys.baselines(projectId ?? ''),
    queryFn: () => get<Baseline[]>(`/projects/${projectId}/baselines`),
    enabled: Boolean(projectId)
  });
}

export function useCaptureBaseline() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, name }: { projectId: string; name?: string }) =>
      api.post<Baseline>(`/projects/${projectId}/baselines`, { name }).then((r) => r.data),
    onSuccess: (_data, variables) => {
      client.invalidateQueries({ queryKey: keys.baselines(variables.projectId) });
      client.invalidateQueries({ queryKey: keys.earnedValue(variables.projectId) });
    }
  });
}

export interface EarnedValueReport {
  baseline: { id: string; name: string; capturedAt: string; tasks: number };
  earnedValue: EarnedValue;
}

export function useEarnedValue(projectId: string | undefined) {
  return useQuery({
    queryKey: keys.earnedValue(projectId ?? ''),
    queryFn: () => get<EarnedValueReport>(`/projects/${projectId}/earned-value`),
    enabled: Boolean(projectId),
    // A project with no baseline answers 404. That is an answer, not a
    // transient failure worth retrying.
    retry: false
  });
}

/* --- Dashboard layout ---------------------------------------------------------- */

export interface DashboardWidget {
  id: string;
  type?: string;
  size?: number;
  hidden?: boolean;
}

export function useDashboardLayout() {
  return useQuery({
    queryKey: keys.dashboardLayout,
    queryFn: () => get<{ layout: DashboardWidget[] | null }>('/dashboard/layout'),
    staleTime: 5 * 60_000
  });
}

export function useSaveDashboardLayout() {
  return useMutation({
    mutationFn: (layout: DashboardWidget[]) => api.put('/dashboard/layout', { layout })
    // Deliberately no invalidation: the browser already holds what it just
    // sent, and a refetch would flash the cards back through the reorder that
    // was only just committed.
  });
}

/* --- Service tokens, SSO and privacy ------------------------------------------- */

export interface ServiceToken {
  id: string;
  name: string;
  scopes: string[];
  createdAt: string;
  lastUsedAt?: string;
  revokedAt?: string;
  /** Present exactly once, in the response that created it. */
  secret?: string;
}

export function useServiceTokens(enabled = true) {
  return useQuery({
    queryKey: keys.serviceTokens,
    queryFn: () => get<ServiceToken[]>('/service-tokens'),
    enabled,
    retry: false
  });
}

export function useCreateServiceToken() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; scopes: string[] }) =>
      api.post<ServiceToken>('/service-tokens', body).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.serviceTokens })
  });
}

export function useRevokeServiceToken() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/service-tokens/${id}`),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.serviceTokens })
  });
}

export function useSSOConfig() {
  return useQuery({
    queryKey: keys.ssoConfig,
    queryFn: () => get<{ enabled: boolean; label: string }>('/auth/oidc/config'),
    staleTime: Infinity,
    retry: false
  });
}

export function useEraseUser() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, confirm }: { id: string; confirm: string }) =>
      api.post(`/users/${id}/erase`, { confirm }),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.users })
  });
}

export function useUpdateUser() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string } & Record<string, unknown>) =>
      api.put<User>(`/users/${id}`, body).then((r) => r.data),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: keys.users });
      client.invalidateQueries({ queryKey: keys.me });
    }
  });
}
