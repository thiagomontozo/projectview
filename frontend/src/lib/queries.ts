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
  Attachment,
  AttachmentConfig,
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
  attachments: (taskId: string) => ['tasks', taskId, 'attachments'] as const,
  attachmentConfig: ['attachments', 'config'] as const,
  runningTimer: ['time', 'running'] as const,
  thread: (messageId: string) => ['chat', 'thread', messageId] as const,
  presence: ['presence'] as const,
  docs: (scopeId: string) => ['docs', scopeId] as const,
  doc: (id: string) => ['docs', 'detail', id] as const,
  docRevisions: (id: string) => ['docs', 'detail', id, 'revisions'] as const,
  notificationPreferences: ['notifications', 'preferences'] as const,
  channels: ['chat', 'channels'] as const,
  messages: (channelId: string) => ['chat', channelId, 'messages'] as const,
  notifications: ['notifications'] as const,
  overview: ['dashboard', 'overview'] as const,
  statusBreakdown: ['dashboard', 'status-breakdown'] as const,
  workloadChart: ['dashboard', 'workload-chart'] as const,
  projectProgress: ['dashboard', 'project-progress'] as const,
  completionTrend: ['dashboard', 'completion-trend'] as const,
  audit: (filters: string) => ['audit', filters] as const,
  goals: (spaceId: string) => ['goals', spaceId] as const,
  goal: (id: string) => ['goals', 'detail', id] as const,
  portfolio: (spaceId: string) => ['portfolio', spaceId] as const,
  capacity: (window: string) => ['portfolio', 'capacity', window] as const,
  baselines: (projectId: string) => ['projects', projectId, 'baselines'] as const,
  earnedValue: (projectId: string) => ['projects', projectId, 'earned-value'] as const,
  dashboardLayout: ['dashboard', 'layout'] as const,
  serviceTokens: ['service-tokens'] as const,
  ssoConfig: ['auth', 'oidc-config'] as const,
  adminSettings: ['settings'] as const
};

/* --- Fetchers ---------------------------------------------------------------- */

export const get = async <T>(url: string, params?: Record<string, unknown>): Promise<T> => {
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
    onSuccess: (_task, { taskId }) => {
      client.invalidateQueries({ queryKey: keys.task(taskId) });
      // A comment can carry files, so the attachment list is stale too.
      client.invalidateQueries({ queryKey: keys.attachments(taskId) });
    }
  });
}

/**
 * Identifies the comment a request just created.
 *
 * The endpoint answers with the whole task rather than the new comment, so the
 * id is found by diffing against the ids that existed before. Deliberately not
 * "take the last one": that assumes an ordering the client does not control,
 * and this needs no assumption at all — the new id is simply the one that was
 * not there a moment ago.
 */
export function newCommentId(before: Task | undefined, after: Task): string | undefined {
  const existing = new Set((before?.comments ?? []).map((c) => c.id));
  return after.comments?.find((c) => !existing.has(c.id))?.id;
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

/* --- Attachments ----------------------------------------------------------------------- */

/**
 * What the server will accept.
 *
 * Fetched once and cached for the session: it comes from the deployment's
 * environment, so it cannot change while somebody has the app open. Knowing it
 * client-side is what lets an oversized file be refused instantly instead of
 * after a minute of uploading.
 */
export function useAttachmentConfig() {
  return useQuery({
    queryKey: keys.attachmentConfig,
    queryFn: () => get<AttachmentConfig>('/attachments/config'),
    staleTime: Infinity,
    gcTime: Infinity
  });
}

export function useAttachments(taskId: string | undefined) {
  return useQuery({
    queryKey: keys.attachments(taskId ?? ''),
    queryFn: () => get<Attachment[]>(`/tasks/${taskId}/attachments`),
    enabled: Boolean(taskId)
  });
}

/**
 * Uploads one file.
 *
 * multipart/form-data rather than a base64 JSON field: base64 inflates the
 * body by a third and forces the whole file through a string in memory on both
 * ends. `onProgress` is wired to axios so a large upload shows movement — a
 * progress bar is the difference between "slow" and "frozen".
 */
export function useUploadAttachment() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      taskId,
      file,
      commentId,
      onProgress
    }: {
      taskId: string;
      file: File;
      commentId?: string;
      onProgress?: (percent: number) => void;
    }) => {
      const form = new FormData();
      form.append('file', file);
      if (commentId) form.append('commentId', commentId);

      return api
        .post<Attachment>(`/tasks/${taskId}/attachments`, form, {
          onUploadProgress: (event) => {
            if (!onProgress || !event.total) return;
            onProgress(Math.round((event.loaded / event.total) * 100));
          }
        })
        .then((r) => r.data);
    },
    onSuccess: (_attachment, { taskId }) => {
      client.invalidateQueries({ queryKey: keys.attachments(taskId) });
      client.invalidateQueries({ queryKey: keys.task(taskId) });
    }
  });
}

export function useDeleteAttachment() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id }: { id: string; taskId: string }) => api.delete(`/attachments/${id}`),
    onSuccess: (_data, { taskId }) => {
      client.invalidateQueries({ queryKey: keys.attachments(taskId) });
      client.invalidateQueries({ queryKey: keys.task(taskId) });
    }
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

/* --- Chat: threads, reactions, presence ------------------------------------------- */

export interface Reaction {
  emoji: string;
  users: string[];
}

export interface ChatMessageDetail extends ChatMessage {
  parentId?: string;
  editedAt?: string;
  reactions: Reaction[];
  replyCount: number;
}

export function useThread(messageId: string | undefined) {
  return useQuery({
    queryKey: keys.thread(messageId ?? ''),
    queryFn: () => get<ChatMessageDetail[]>(`/chat/messages/${messageId}/replies`),
    enabled: Boolean(messageId)
  });
}

export function useReply() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ messageId, body }: { messageId: string; body: string; channelId: string }) =>
      api.post<ChatMessageDetail>(`/chat/messages/${messageId}/replies`, { body }).then((r) => r.data),
    onSuccess: (_reply, { messageId, channelId }) => {
      client.invalidateQueries({ queryKey: keys.thread(messageId) });
      // The parent's reply count changed, so the channel list is stale too.
      client.invalidateQueries({ queryKey: keys.messages(channelId) });
    }
  });
}

export function useToggleReaction() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ messageId, emoji }: { messageId: string; emoji: string; channelId: string }) =>
      api.post<{ added: boolean }>(`/chat/messages/${messageId}/reactions`, { emoji }).then((r) => r.data),
    onSuccess: (_result, { channelId }) => client.invalidateQueries({ queryKey: keys.messages(channelId) })
  });
}

export function usePresence() {
  return useQuery({
    queryKey: keys.presence,
    queryFn: () => get<string[]>('/presence'),
    // Presence also arrives over the socket; this is the initial state and a
    // safety net for a client that reconnected and missed events.
    refetchInterval: 60_000
  });
}

/* --- Docs -------------------------------------------------------------------------- */

export interface Doc {
  id: string;
  spaceId?: string;
  projectId?: string;
  parentId?: string;
  title: string;
  content: string;
  position: number;
  archived: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface DocRevision {
  id: number;
  title: string;
  authorId?: string;
  createdAt: string;
  /** Only the single-revision endpoint returns the text. */
  content?: string;
}

export function useDocs(scope: { spaceId?: string; projectId?: string }) {
  const key = scope.spaceId ?? scope.projectId ?? '';
  return useQuery({
    queryKey: keys.docs(key),
    queryFn: () => get<Doc[]>('/docs', scope),
    enabled: Boolean(key)
  });
}

export function useDoc(id: string | undefined) {
  return useQuery({
    queryKey: keys.doc(id ?? ''),
    queryFn: () => get<Doc>(`/docs/${id}`),
    enabled: Boolean(id)
  });
}

export function useCreateDoc() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: Partial<Doc>) => api.post<Doc>('/docs', body).then((r) => r.data),
    onSuccess: (doc) => client.invalidateQueries({ queryKey: keys.docs(doc.spaceId ?? doc.projectId ?? '') })
  });
}

export function useSaveDoc() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; title?: string; content?: string; archived?: boolean }) =>
      api.put<Doc>(`/docs/${id}`, body).then((r) => r.data),
    onSuccess: (doc) => {
      client.invalidateQueries({ queryKey: keys.doc(doc.id) });
      client.invalidateQueries({ queryKey: keys.docs(doc.spaceId ?? doc.projectId ?? '') });
    }
  });
}

export function useDocRevisions(id: string | undefined) {
  return useQuery({
    queryKey: keys.docRevisions(id ?? ''),
    queryFn: () => get<DocRevision[]>(`/docs/${id}/revisions`),
    enabled: Boolean(id)
  });
}

/**
 * Loads one past version, on demand.
 *
 * The listing deliberately omits bodies, so restoring means fetching the
 * chosen revision by itself rather than holding every version in memory for a
 * button nobody may press.
 */
export function useDocRevision(docId: string | undefined, revisionId: number | undefined) {
  return useQuery({
    queryKey: [...keys.docRevisions(docId ?? ''), revisionId],
    queryFn: () => get<DocRevision>(`/docs/${docId}/revisions/${revisionId}`),
    enabled: Boolean(docId && revisionId)
  });
}

/* --- Notification preferences ------------------------------------------------------- */

export interface ChannelPreference {
  inApp: boolean;
  email: boolean;
}

export interface NotificationPreferences {
  channels: Record<string, ChannelPreference>;
  digest: 'off' | 'daily' | 'weekly';
  digestHour: number;
  quietStart?: number;
  quietEnd?: number;
}

export function useNotificationPreferences() {
  return useQuery({
    queryKey: keys.notificationPreferences,
    queryFn: () => get<NotificationPreferences>('/notifications/preferences'),
    staleTime: 5 * 60_000
  });
}

export function useSaveNotificationPreferences() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (prefs: NotificationPreferences) =>
      api.put<NotificationPreferences>('/notifications/preferences', prefs).then((r) => r.data),
    onSuccess: (prefs) => client.setQueryData(keys.notificationPreferences, prefs)
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
