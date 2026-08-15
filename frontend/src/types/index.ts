// Shared types mirroring the Go backend's JSON responses
// (see backend/internal/models, internal/repo and internal/handlers).

export type Role = 'admin' | 'manager' | 'member';
export type AuthSource = 'local' | 'ad' | 'oidc' | 'scim';
export type Priority = 'low' | 'medium' | 'high' | 'urgent';
export type ProjectStatusState = 'planning' | 'active' | 'on_hold' | 'completed' | 'archived';
/** Role held on a Space; grants flow down to everything inside it. */
export type SpaceRole = 'owner' | 'admin' | 'member' | 'guest';

export interface PublicUser {
  id: string;
  name: string;
  email: string;
  avatarColor: string;
  role?: Role;
  title?: string;
}

export interface User extends PublicUser {
  username: string;
  authSource: AuthSource;
  role: Role;
  teams: string[];
  active: boolean;
  notifyByEmail: boolean;
  /** Hours a week this person is available for; capacity planning uses it. */
  weeklyCapacityHours: number;
  lastLoginAt?: string;
  createdAt: string;
  updatedAt: string;
}

/** A live login. Listed so a person can see where they are signed in. */
export interface Session {
  id: string;
  userAgent?: string;
  ip?: string;
  current: boolean;
  lastUsedAt?: string;
  expiresAt: string;
  createdAt: string;
}

export interface Team {
  id: string;
  name: string;
  description?: string;
  color: string;
  members: PublicUser[];
  leadId?: PublicUser;
  createdAt: string;
  updatedAt: string;
}

export interface SpaceMember {
  user: PublicUser;
  role: SpaceRole;
}

/** Top of the hierarchy: Space -> Folder -> List(project) -> Task. */
export interface Space {
  id: string;
  name: string;
  description?: string;
  color: string;
  isPrivate: boolean;
  position: number;
  archived: boolean;
  members: SpaceMember[];
  /** The caller's effective role, so the UI can hide what would be refused. */
  yourRole?: SpaceRole;
  createdAt: string;
  updatedAt: string;
}

export interface Folder {
  id: string;
  spaceId: string;
  name: string;
  color: string;
  position: number;
  archived: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectStatusColumn {
  key: string;
  label: string;
  order: number;
  color: string;
}

export interface TeamRef {
  id: string;
  name: string;
  color: string;
}

export interface Project {
  id: string;
  name: string;
  key: string;
  description?: string;
  color: string;
  status: ProjectStatusState;
  spaceId?: string;
  folderId?: string;
  position: number;
  archived: boolean;
  team?: TeamRef;
  members: PublicUser[];
  owner?: PublicUser;
  startDate?: string;
  endDate?: string;
  statuses: ProjectStatusColumn[];
  createdAt: string;
  updatedAt: string;
}

export interface ProjectRefLite {
  id: string;
  name: string;
  key: string;
  color: string;
}

export interface ChecklistItem {
  id: string;
  text: string;
  done: boolean;
}

export interface Comment {
  id: string;
  author?: PublicUser;
  body: string;
  createdAt: string;
}

export interface Task {
  id: string;
  title: string;
  description?: string;
  project: string | ProjectRefLite;
  parentTask?: string | null;
  assignees: PublicUser[];
  status: string;
  priority: Priority;
  startDate?: string;
  dueDate?: string;
  completedAt?: string;
  estimateHours: number;
  order: number;
  tags: string[];
  checklist: ChecklistItem[];
  comments: Comment[];
  createdBy?: PublicUser;
  subtasks?: Task[];
  subtaskCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface ChatChannel {
  id: string;
  name?: string;
  type: 'team' | 'project' | 'dm';
  team?: TeamRef;
  project?: ProjectRefLite;
  members: PublicUser[];
  createdAt: string;
  updatedAt: string;
}

export interface ChatMessage {
  id: string;
  channel: string;
  author?: PublicUser;
  body: string;
  readBy: string[];
  createdAt: string;
}

export type NotificationType =
  | 'task_due_soon'
  | 'task_overdue'
  | 'task_assigned'
  | 'comment_mention'
  | 'chat_message'
  | 'general';

export interface AppNotification {
  id: string;
  user: string;
  type: NotificationType;
  title: string;
  body?: string;
  task?: string;
  project?: string;
  read: boolean;
  createdAt: string;
}

export interface WorkloadRow {
  user: User;
  openTasks: number;
  estimateHours: number;
  overdue: number;
  projectCount: number;
}

export interface DashboardOverview {
  totalProjects: number;
  activeProjects: number;
  totalTasks: number;
  doneTasks: number;
  overdueTasks: number;
}

export interface StatusBreakdownRow {
  status: string;
  count: number;
}

export interface WorkloadChartRow {
  name: string;
  count: number;
}

export interface ProjectProgressRow {
  project: ProjectRefLite;
  total: number;
  done: number;
  percent: number;
}

export interface CompletionTrendRow {
  date: string;
  count: number;
}

/** Cursor-paginated envelope returned by the searchable listings. */
export interface Page<T> {
  items: T[];
  nextCursor?: string;
  hasMore: boolean;
  total?: number;
}

/** One entry of the append-only audit trail. */
export interface AuditEntry {
  id: number;
  occurredAt: string;
  actorId?: string;
  actor: string;
  action: string;
  resourceType: string;
  resourceId?: string;
  changes?: Record<string, unknown>;
  ip?: string;
  requestId?: string;
  status: number;
}

// Envelope pushed by the backend over the WebSocket ("/ws?token=...").
export interface RealtimeMessage {
  type: 'notification' | 'chat:message' | 'chat:reaction' | 'presence' | 'typing';
  payload: unknown;
}
