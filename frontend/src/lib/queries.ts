import {
  QueryClient,
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryOptions
} from '@tanstack/react-query';
import { api, statusOf } from './api';
import type {
  AppNotification,
  ChatChannel,
  ChatMessage,
  DashboardOverview,
  Folder,
  Page,
  Project,
  ProjectProgressRow,
  PublicUser,
  Session,
  Space,
  StatusBreakdownRow,
  Task,
  Team,
  User,
  WorkloadChartRow,
  WorkloadRow,
  CompletionTrendRow
} from '../types';

/* -----------------------------------------------------------------------------
 * Query client
 *
 * Replaces the per-page useEffect + fetch pattern, which re-requested the same
 * data on every mount, had no cache to share between pages, and offered no
 * retry, no deduplication and no way to invalidate after a write.
 * -------------------------------------------------------------------------- */

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        gcTime: 5 * 60_000,
        refetchOnWindowFocus: true,
        retry: (failureCount, error) => {
          const status = statusOf(error);
          // Retrying a 4xx just repeats a request the server already
          // answered definitively; only transient failures are worth another
          // attempt.
          if (status && status >= 400 && status < 500) return false;
          return failureCount < 2;
        }
      },
      mutations: { retry: false }
    }
  });
}

/* -----------------------------------------------------------------------------
 * Query keys
 *
 * Centralised so invalidation after a write cannot drift from the key a query
 * actually registered under — the usual source of "the list did not update".
 * -------------------------------------------------------------------------- */

export const keys = {
  me: ['me'] as const,
  sessions: ['sessions'] as const,
  users: ['users'] as const,
  workload: ['users', 'workload'] as const,
  teams: ['teams'] as const,
  spaces: ['spaces'] as const,
  folders: (spaceId: string) => ['spaces', spaceId, 'folders'] as const,
  projects: ['projects'] as const,
  project: (id: string) => ['projects', id] as const,
  projectTasks: (id: string) => ['projects', id, 'tasks'] as const,
  task: (id: string) => ['tasks', id] as const,
  myTasks: ['tasks', 'mine'] as const,
  taskSearch: (query: string) => ['tasks', 'search', query] as const,
  schedule: (projectId: string) => ['projects', projectId, 'schedule'] as const,
  customFields: (projectId: string) => ['projects', projectId, 'fields'] as const,
  taskTime: (taskId: string) => ['tasks', taskId, 'time'] as const,
  runningTimer: ['time', 'running'] as const,
  channels: ['chat', 'channels'] as const,
  messages: (channelId: string) => ['chat', channelId, 'messages'] as const,
  notifications: ['notifications'] as const,
  overview: ['dashboard', 'overview'] as const,
  statusBreakdown: ['dashboard', 'status-breakdown'] as const,
  workloadChart: ['dashboard', 'workload-chart'] as const,
  projectProgress: ['dashboard', 'project-progress'] as const,
  completionTrend: ['dashboard', 'completion-trend'] as const,
  audit: (filters: string) => ['audit', filters] as const
};

/* --- Fetchers ---------------------------------------------------------------- */

const get = async <T>(url: string, params?: Record<string, unknown>): Promise<T> => {
  const { data } = await api.get<T>(url, { params });
  return data;
};

/* --- Identity ---------------------------------------------------------------- */

export function useMe(options?: Partial<UseQueryOptions<{ user: User }>>) {
  return useQuery({
    queryKey: keys.me,
    queryFn: () => get<{ user: User }>('/auth/me'),
    // The session either exists or it does not; retrying a 401 only delays
    // the redirect to the login screen.
    retry: false,
    staleTime: 5 * 60_000,
    ...options
  });
}

export function useSessions() {
  return useQuery({ queryKey: keys.sessions, queryFn: () => get<Session[]>('/auth/sessions') });
}

export function useRevokeSession() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/auth/sessions/${id}`),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.sessions })
  });
}

/* --- People ------------------------------------------------------------------ */

export function useUsers() {
  return useQuery({ queryKey: keys.users, queryFn: () => get<User[]>('/users'), staleTime: 5 * 60_000 });
}

export function useWorkload() {
  return useQuery({ queryKey: keys.workload, queryFn: () => get<WorkloadRow[]>('/users/workload') });
}

export function useTeams() {
  return useQuery({ queryKey: keys.teams, queryFn: () => get<Team[]>('/teams') });
}

export function useCreateTeam() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; description?: string; memberIds?: string[] }) =>
      api.post<Team>('/teams', body).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.teams })
  });
}

/* --- Hierarchy ---------------------------------------------------------------- */

export function useSpaces() {
  return useQuery({ queryKey: keys.spaces, queryFn: () => get<Space[]>('/spaces') });
}

export function useFolders(spaceId: string | undefined) {
  return useQuery({
    queryKey: keys.folders(spaceId ?? ''),
    queryFn: () => get<Folder[]>(`/spaces/${spaceId}/folders`),
    enabled: Boolean(spaceId)
  });
}

export function useCreateSpace() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; description?: string; isPrivate?: boolean }) =>
      api.post<Space>('/spaces', body).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.spaces })
  });
}

/* --- Projects ----------------------------------------------------------------- */

export function useProjects() {
  return useQuery({ queryKey: keys.projects, queryFn: () => get<Project[]>('/projects') });
}

export function useProject(id: string | undefined) {
  return useQuery({
    queryKey: keys.project(id ?? ''),
    queryFn: () => get<Project>(`/projects/${id}`),
    enabled: Boolean(id)
  });
}

export function useCreateProject() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.post<Project>('/projects', body).then((r) => r.data),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: keys.projects });
      client.invalidateQueries({ queryKey: keys.overview });
    }
  });
}

/* --- Tasks --------------------------------------------------------------------- */

export function useProjectTasks(projectId: string | undefined) {
  return useQuery({
    queryKey: keys.projectTasks(projectId ?? ''),
    queryFn: () => get<Task[]>(`/projects/${projectId}/tasks`),
    enabled: Boolean(projectId)
  });
}

export function useMyTasks() {
  return useQuery({ queryKey: keys.myTasks, queryFn: () => get<Task[]>('/tasks/mine') });
}

/** Full-text search across tasks, backed by the server's tsvector index. */
export function useTaskSearch(query: string, enabled = true) {
  return useQuery({
    queryKey: keys.taskSearch(query),
    queryFn: () => get<Page<Task>>('/tasks', { q: query, limit: 8 }),
    enabled: enabled && query.trim().length > 1,
    staleTime: 15_000
  });
}

export function useTask(id: string | undefined) {
  return useQuery({
    queryKey: keys.task(id ?? ''),
    queryFn: () => get<Task>(`/tasks/${id}`),
    enabled: Boolean(id)
  });
}

interface MoveTaskInput {
  taskId: string;
  projectId: string;
  status: string;
  order: number;
}

/**
 * Moves a card on the board.
 *
 * Optimistic: the card moves the instant it is dropped, and the previous board
 * is restored if the request fails. Waiting for a round trip before the card
 * moves makes dragging feel broken even on a fast connection.
 */
export function useMoveTask() {
  const client = useQueryClient();

  return useMutation({
    mutationFn: ({ taskId, status, order }: MoveTaskInput) =>
      api.patch(`/tasks/${taskId}/move`, { status, order }),

    onMutate: async ({ taskId, projectId, status, order }) => {
      const key = keys.projectTasks(projectId);
      // Cancel in-flight refetches, or one could land after the optimistic
      // write and overwrite it with stale data.
      await client.cancelQueries({ queryKey: key });

      const previous = client.getQueryData<Task[]>(key);
      client.setQueryData<Task[]>(key, (tasks) =>
        tasks?.map((task) => (task.id === taskId ? { ...task, status, order } : task))
      );
      return { previous, key };
    },

    onError: (_error, _input, context) => {
      if (context?.previous) {
        client.setQueryData(context.key, context.previous);
      }
    },

    onSettled: (_data, _error, { projectId }) => {
      client.invalidateQueries({ queryKey: keys.projectTasks(projectId) });
      client.invalidateQueries({ queryKey: keys.statusBreakdown });
      client.invalidateQueries({ queryKey: keys.overview });
    }
  });
}

export function useSaveTask() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id?: string; body: Record<string, unknown> }) =>
      id ? api.put<Task>(`/tasks/${id}`, body).then((r) => r.data) : api.post<Task>('/tasks', body).then((r) => r.data),
    onSuccess: (task) => {
      const projectId = typeof task.project === 'string' ? task.project : task.project?.id;
      if (projectId) client.invalidateQueries({ queryKey: keys.projectTasks(projectId) });
      client.invalidateQueries({ queryKey: keys.myTasks });
      client.invalidateQueries({ queryKey: keys.overview });
    }
  });
}

export function useDeleteTask() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/tasks/${id}`),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: keys.projects });
      client.invalidateQueries({ queryKey: ['projects'] });
      client.invalidateQueries({ queryKey: keys.myTasks });
    }
  });
}

export function useAddComment() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId, body }: { taskId: string; body: string }) =>
      api.post<Task>(`/tasks/${taskId}/comments`, { body }).then((r) => r.data),
    onSuccess: (_task, { taskId }) => client.invalidateQueries({ queryKey: keys.task(taskId) })
  });
}

/* --- Schedule: dependencies and the critical path -------------------------------- */

export interface Dependency {
  taskId: string;
  dependsOn: string;
  type: string;
  lagDays: number;
}

export interface BlockedTask {
  taskId: string;
  blockerId: string;
  blockerTitle: string;
}

export interface Schedule {
  dependencies: Dependency[];
  /** Ids on the longest weighted chain — where a slip moves the end date. */
  criticalPath: string[];
  blocked: BlockedTask[];
}

export function useSchedule(projectId: string | undefined) {
  return useQuery({
    queryKey: keys.schedule(projectId ?? ''),
    queryFn: () => get<Schedule>(`/projects/${projectId}/schedule`),
    enabled: Boolean(projectId)
  });
}

export function useAddDependency() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId, dependsOn }: { taskId: string; dependsOn: string; projectId: string }) =>
      api.post(`/tasks/${taskId}/dependencies`, { dependsOn }),
    onSuccess: (_data, { projectId }) => client.invalidateQueries({ queryKey: keys.schedule(projectId) })
  });
}

export function useRemoveDependency() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId, dependsOn }: { taskId: string; dependsOn: string; projectId: string }) =>
      api.delete(`/tasks/${taskId}/dependencies/${dependsOn}`),
    onSuccess: (_data, { projectId }) => client.invalidateQueries({ queryKey: keys.schedule(projectId) })
  });
}

/* --- Custom fields ---------------------------------------------------------------- */

export interface FieldDefinition {
  id: string;
  key: string;
  label: string;
  type: 'text' | 'number' | 'date' | 'select' | 'multi_select' | 'checkbox' | 'url' | 'email' | 'user';
  options: string[];
  required: boolean;
  position: number;
}

export function useCustomFields(projectId: string | undefined) {
  return useQuery({
    queryKey: keys.customFields(projectId ?? ''),
    queryFn: () => get<FieldDefinition[]>(`/projects/${projectId}/fields`),
    enabled: Boolean(projectId),
    staleTime: 5 * 60_000
  });
}

export function useSetTaskFields() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId, values }: { taskId: string; values: Record<string, unknown> }) =>
      api.put<Task>(`/tasks/${taskId}/fields`, values).then((r) => r.data),
    onSuccess: (_task, { taskId }) => client.invalidateQueries({ queryKey: keys.task(taskId) })
  });
}

/* --- Time tracking ------------------------------------------------------------------ */

export interface TimeEntry {
  id: string;
  taskId: string;
  userId: string;
  startedAt: string;
  endedAt?: string;
  seconds: number;
  note?: string;
  user?: PublicUser;
}

export function useRunningTimer() {
  return useQuery({
    queryKey: keys.runningTimer,
    queryFn: () => get<TimeEntry | null>('/time/running'),
    // A running timer is wall-clock state, so it is refreshed on an interval
    // rather than trusted to stay accurate.
    refetchInterval: 30_000
  });
}

export function useTaskTime(taskId: string | undefined) {
  return useQuery({
    queryKey: keys.taskTime(taskId ?? ''),
    queryFn: () => get<TimeEntry[]>(`/tasks/${taskId}/time`),
    enabled: Boolean(taskId)
  });
}

export function useStartTimer() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (taskId: string) => api.post<TimeEntry>(`/tasks/${taskId}/time/start`).then((r) => r.data),
    onSuccess: (_entry, taskId) => {
      client.invalidateQueries({ queryKey: keys.runningTimer });
      client.invalidateQueries({ queryKey: keys.taskTime(taskId) });
    }
  });
}

export function useStopTimer() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<TimeEntry>('/time/stop').then((r) => r.data),
    onSuccess: (entry) => {
      client.invalidateQueries({ queryKey: keys.runningTimer });
      if (entry?.taskId) client.invalidateQueries({ queryKey: keys.taskTime(entry.taskId) });
    }
  });
}

export function useLogTime() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId, minutes, note }: { taskId: string; minutes: number; note?: string }) =>
      api.post<TimeEntry>(`/tasks/${taskId}/time`, { minutes, note }).then((r) => r.data),
    onSuccess: (_entry, { taskId }) => client.invalidateQueries({ queryKey: keys.taskTime(taskId) })
  });
}

/* --- Watchers -------------------------------------------------------------------------- */

export function useWatchTask() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId, watching }: { taskId: string; watching: boolean }) =>
      watching ? api.post(`/tasks/${taskId}/watch`) : api.delete(`/tasks/${taskId}/watch`),
    onSuccess: (_data, { taskId }) => client.invalidateQueries({ queryKey: keys.task(taskId) })
  });
}

/* --- Chat ----------------------------------------------------------------------- */

export function useChannels() {
  return useQuery({ queryKey: keys.channels, queryFn: () => get<ChatChannel[]>('/chat/channels') });
}

export function useMessages(channelId: string | undefined) {
  return useQuery({
    queryKey: keys.messages(channelId ?? ''),
    queryFn: () => get<ChatMessage[]>(`/chat/channels/${channelId}/messages`),
    enabled: Boolean(channelId)
  });
}

export function usePostMessage() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ channelId, body }: { channelId: string; body: string }) =>
      api.post<ChatMessage>(`/chat/channels/${channelId}/messages`, { body }).then((r) => r.data),
    onSuccess: (_message, { channelId }) => {
      client.invalidateQueries({ queryKey: keys.messages(channelId) });
      client.invalidateQueries({ queryKey: keys.channels });
    }
  });
}

/* --- Notifications ---------------------------------------------------------------- */

export function useNotifications() {
  return useQuery({
    queryKey: keys.notifications,
    queryFn: () => get<AppNotification[]>('/notifications'),
    staleTime: 15_000
  });
}

export function useMarkAllNotificationsRead() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => api.post('/notifications/read-all'),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.notifications })
  });
}

/* --- Dashboard ---------------------------------------------------------------------- */

export function useOverview() {
  return useQuery({ queryKey: keys.overview, queryFn: () => get<DashboardOverview>('/dashboard/overview') });
}

export function useStatusBreakdown() {
  return useQuery({
    queryKey: keys.statusBreakdown,
    queryFn: () => get<StatusBreakdownRow[]>('/dashboard/status-breakdown')
  });
}

export function useWorkloadChart() {
  return useQuery({
    queryKey: keys.workloadChart,
    queryFn: () => get<WorkloadChartRow[]>('/dashboard/workload-chart')
  });
}

export function useProjectProgress() {
  return useQuery({
    queryKey: keys.projectProgress,
    queryFn: () => get<ProjectProgressRow[]>('/dashboard/project-progress')
  });
}

export function useCompletionTrend() {
  return useQuery({
    queryKey: keys.completionTrend,
    queryFn: () => get<CompletionTrendRow[]>('/dashboard/completion-trend')
  });
}
