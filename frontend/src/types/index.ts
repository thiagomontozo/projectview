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

/**
 * Whether an uploaded file was examined.
 *
 * `skipped` is not `clean`: it means no scanner is wired in, so nothing looked
 * at the file. The interface says so rather than implying a check that never
 * happened.
 */
export type ScanStatus = 'pending' | 'clean' | 'infected' | 'skipped';

/** A file on a task, or on one of its comments. */
export interface Attachment {
  id: string;
  taskId: string;
  commentId?: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  checksum: string;
  scanStatus: ScanStatus;
  scannedAt?: string;
  uploadedBy?: PublicUser;
  /**
   * This API's redirect, not the signed object URL. Following it mints a
   * short-lived link at the moment somebody actually asks for the file, rather
   * than handing out a live capability for every attachment on the page.
   */
  downloadUrl: string;
  /** Safe for the browser to render in place (images and PDFs). */
  inline: boolean;
  createdAt: string;
}

/** What the server will accept, so a file can be refused before it is sent. */
export interface AttachmentConfig {
  enabled: boolean;
  maxBytes: number;
  maxTaskBytes: number;
  allowedTypes: string[] | null;
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

/* --- Intake ---------------------------------------------------------------- */

export interface IntakeField {
  key: string;
  label: string;
  type: 'text' | 'textarea' | 'number' | 'date' | 'select' | 'checkbox' | 'email';
  required: boolean;
  options?: string[];
  help?: string;
}

export interface IntakeForm {
  id: string;
  projectId: string;
  title: string;
  description: string;
  fields: IntakeField[];
  targetStatus: string;
  targetPriority: string;
  enabled: boolean;
  public: boolean;
  /** The unguessable part of the public address. 128 bits, not a name. */
  slug: string;
  createdAt: string;
  updatedAt: string;
}

/**
 * What a model proposed about a submission.
 *
 * Every field is optional because "the model had nothing usable to say" is a
 * normal outcome, not an error: the server validates the reply against the
 * project's own people and the four real priorities, and drops whatever is
 * left over.
 */
export interface TriageSuggestion {
  priority?: string;
  assigneeId?: string;
  assigneeName?: string;
  summary?: string;
  model?: string;
  tokens?: number;
}

export interface IntakeSubmission {
  id: string;
  formId: string;
  taskId?: string;
  answers: Record<string, unknown>;
  submitterName?: string;
  submitterEmail?: string;
  createdAt: string;
  suggestion?: TriageSuggestion | null;
  /** Set once the model has been asked, whether or not it answered usefully. */
  suggestedAt?: string | null;
  /** Set when a person applied the suggestion. */
  acceptedAt?: string | null;
}
