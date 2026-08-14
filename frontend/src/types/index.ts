// Shared types mirroring the Go backend's JSON responses
// (see backend/internal/models and backend/internal/handlers).

export type Role = 'admin' | 'manager' | 'member';
export type AuthSource = 'local' | 'ad';
export type Priority = 'low' | 'medium' | 'high' | 'urgent';
export type ProjectStatusState = 'planning' | 'active' | 'on_hold' | 'completed' | 'archived';

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
  lastLoginAt?: string;
  createdAt: string;
  updatedAt: string;
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
  author: PublicUser;
  body: string;
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

// Envelope pushed by the backend over the WebSocket ("/ws?token=...").
export interface RealtimeMessage {
  type: 'notification' | 'chat:message';
  payload: unknown;
}
