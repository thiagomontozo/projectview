import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import * as SelectPrimitive from '@radix-ui/react-select';
import clsx from 'clsx';
import Attachments, { AttachmentList, CommentAttachmentPicker } from './Attachments';
import RecurrenceSection from './RecurrenceSection';
import { Dialog, ConfirmDialog } from '../ui/Dialog';
import { Button } from '../ui/Button';
import { Field, Input, Textarea } from '../ui/Field';
import { Avatar, Badge } from '../ui/display';
import { useToast } from '../ui/Toast';
import { Check, ChevronDown } from '../ui/icons';
import {
  newCommentId,
  useAddComment,
  useAttachments,
  useCustomFields,
  useDeleteTask,
  useLogTime,
  useRunningTimer,
  useSaveTask,
  useSetTaskFields,
  useStartTimer,
  useStopTimer,
  useTask,
  useTaskTime,
  useUploadAttachment
} from '../lib/queries';
import { errorMessage } from '../lib/api';
import { toDateInput } from '../lib/format';
import controls from '../ui/controls.module.css';
import type { Project, PublicUser, Task } from '../types';

interface Props {
  project: Project;
  task?: Task;
  defaultStatus?: string;
  users: PublicUser[];
  onClose: () => void;
}

export default function TaskModal({ project, task, defaultStatus, users, onClose }: Props) {
  const { t } = useTranslation();
  const toast = useToast();
  const isEdit = Boolean(task?.id);

  // Re-fetched so sub-tasks and comments are current, not whatever the board
  // happened to hold.
  const detail = useTask(isEdit ? task!.id : undefined);
  const current = detail.data ?? task;

  const saveTask = useSaveTask();
  const deleteTask = useDeleteTask();
  const addComment = useAddComment();
  const uploadAttachment = useUploadAttachment();
  const { data: attachments = [] } = useAttachments(isEdit ? task!.id : undefined);

  const [form, setForm] = useState({
    title: task?.title ?? '',
    description: task?.description ?? '',
    status: task?.status ?? defaultStatus ?? project.statuses[0]?.key ?? '',
    priority: task?.priority ?? 'medium',
    assignees: task?.assignees?.map((a) => a.id) ?? [],
    startDate: toDateInput(task?.startDate),
    dueDate: toDateInput(task?.dueDate),
    estimateHours: task?.estimateHours ?? 0
  });
  const [comment, setComment] = useState('');
  const [error, setError] = useState('');
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [commentFiles, setCommentFiles] = useState<File[]>([]);

  /**
   * Posts the comment, then attaches whatever was chosen alongside it.
   *
   * In that order because a file has to belong to something: the comment is
   * created first, its id is read back by diffing the returned task against
   * the one held before, and the uploads are pointed at it. Attaching to the
   * task first and re-pointing afterwards would strand a file on the task
   * every time the comment itself failed to save.
   */
  function sendComment() {
    if (!task || !comment.trim()) return;
    const before = current;

    addComment.mutate(
      { taskId: task.id, body: comment },
      {
        onSuccess: (updated) => {
          setComment('');
          const commentId = newCommentId(before, updated);
          const pending = commentFiles;
          setCommentFiles([]);

          // Without an id the files would silently attach to the task instead
          // of the comment, which is not what was asked for — so say so rather
          // than quietly doing something else.
          if (!commentId) {
            if (pending.length > 0) setError(t('attachments.commentUploadFailed'));
            return;
          }
          for (const file of pending) {
            uploadAttachment.mutate(
              { taskId: task.id, file, commentId },
              { onError: (err) => setError(errorMessage(err, t('errors.genericBody'))) }
            );
          }
        },
        onError: (err) => setError(errorMessage(err, t('errors.genericBody')))
      }
    );
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!form.title.trim()) return;
    setError('');

    saveTask.mutate(
      {
        id: task?.id,
        body: {
          ...form,
          startDate: form.startDate ? new Date(form.startDate).toISOString() : null,
          dueDate: form.dueDate ? new Date(form.dueDate).toISOString() : null,
          project: project.id,
          parentTask: task?.parentTask ?? null
        }
      },
      {
        onSuccess: () => {
          toast.success(t('task.saved'));
          onClose();
        },
        onError: (err) => setError(errorMessage(err, t('errors.genericBody')))
      }
    );
  }

  function toggleAssignee(userId: string) {
    setForm((f) => ({
      ...f,
      assignees: f.assignees.includes(userId)
        ? f.assignees.filter((id) => id !== userId)
        : [...f.assignees, userId]
    }));
  }

  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => !open && onClose()}
        size="lg"
        title={isEdit ? t('task.edit') : t('task.new')}
        footer={
          <>
            {isEdit ? (
              <Button variant="dangerGhost" onClick={() => setConfirmDelete(true)}>
                {t('common.delete')}
              </Button>
            ) : (
              <span />
            )}
            <div style={{ display: 'flex', gap: 'var(--space-2)', marginLeft: 'auto' }}>
              <Button variant="ghost" onClick={onClose}>
                {t('common.cancel')}
              </Button>
              <Button variant="primary" type="submit" form="task-form" loading={saveTask.isPending}>
                {t('common.save')}
              </Button>
            </div>
          </>
        }
      >
        <form id="task-form" onSubmit={submit} style={{ display: 'grid', gap: 'var(--space-4)' }}>
          <Field label={t('task.title')} required>
            {({ id }) => (
              <Input
                id={id}
                value={form.title}
                onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
                required
                autoFocus
              />
            )}
          </Field>

          <Field label={t('task.description')}>
            {({ id }) => (
              <Textarea
                id={id}
                value={form.description}
                onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
              />
            )}
          </Field>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 'var(--space-4)' }}>
            <Field label={t('task.status')}>
              {({ id }) => (
                <SelectField
                  id={id}
                  value={form.status}
                  onChange={(value) => setForm((f) => ({ ...f, status: value }))}
                  options={project.statuses.map((s) => ({ value: s.key, label: s.label }))}
                />
              )}
            </Field>

            <Field label={t('task.priority')}>
              {({ id }) => (
                <SelectField
                  id={id}
                  value={form.priority}
                  onChange={(value) => setForm((f) => ({ ...f, priority: value as typeof form.priority }))}
                  options={[
                    { value: 'low', label: t('task.priorityLow') },
                    { value: 'medium', label: t('task.priorityMedium') },
                    { value: 'high', label: t('task.priorityHigh') },
                    { value: 'urgent', label: t('task.priorityUrgent') }
                  ]}
                />
              )}
            </Field>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 'var(--space-4)' }}>
            <Field label={t('task.start')}>
              {({ id }) => (
                <Input
                  id={id}
                  type="date"
                  value={form.startDate}
                  onChange={(e) => setForm((f) => ({ ...f, startDate: e.target.value }))}
                />
              )}
            </Field>
            <Field label={t('task.due')}>
              {({ id }) => (
                <Input
                  id={id}
                  type="date"
                  value={form.dueDate}
                  onChange={(e) => setForm((f) => ({ ...f, dueDate: e.target.value }))}
                />
              )}
            </Field>
            <Field label={t('task.estimate')}>
              {({ id }) => (
                <Input
                  id={id}
                  type="number"
                  min={0}
                  step={0.5}
                  value={form.estimateHours}
                  onChange={(e) => setForm((f) => ({ ...f, estimateHours: Number(e.target.value) }))}
                />
              )}
            </Field>
          </div>

          <fieldset style={{ border: 'none', padding: 0, margin: 0 }}>
            <legend className={controls.label} style={{ marginBottom: 'var(--space-2)' }}>
              {t('task.assignees')}
            </legend>
            <p className={controls.hint} style={{ marginBottom: 'var(--space-2)' }}>
              {t('task.assigneesHint')}
            </p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>
              {users.map((user) => {
                const selected = form.assignees.includes(user.id);
                return (
                  <Button
                    key={user.id}
                    type="button"
                    size="sm"
                    variant={selected ? 'primary' : 'secondary'}
                    aria-pressed={selected}
                    onClick={() => toggleAssignee(user.id)}
                  >
                    <Avatar name={user.name} color={user.avatarColor} size={18} />
                    {user.name}
                  </Button>
                );
              })}
            </div>
          </fieldset>

          {error && (
            <p role="alert" style={{ color: 'var(--danger)', fontSize: 'var(--text-sm)' }}>
              {error}
            </p>
          )}
        </form>

        {isEdit && current?.subtasks && current.subtasks.length > 0 && (
          <section>
            <h4 className={controls.label}>{t('task.subtasks')}</h4>
            <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 'var(--space-2)' }}>
              {current.subtasks.map((subtask) => (
                <li key={subtask.id} style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                  <span>{subtask.title}</span>
                  <Badge>{subtask.status}</Badge>
                </li>
              ))}
            </ul>
          </section>
        )}

        {isEdit && task && <CustomFieldsSection projectId={project.id} task={current ?? task} />}

        {isEdit && task && <TimeSection task={current ?? task} />}

        {/* Only once the task exists: a file has to be attached to something,
            and there is no id to attach it to before the first save. */}
        {isEdit && task && <Attachments taskId={task.id} />}

        {/* Likewise: a recurrence rule needs a task to sit on. */}
        {isEdit && task && <RecurrenceSection task={current ?? task} />}

        {isEdit && task && (
          <section>
            <h4 className={controls.label}>{t('task.comments')}</h4>
            <ul style={{ listStyle: 'none', padding: 0, margin: '0 0 var(--space-3)', display: 'grid', gap: 'var(--space-3)' }}>
              {current?.comments?.map((entry) => (
                <li key={entry.id} style={{ fontSize: 'var(--text-sm)' }}>
                  <strong>{entry.author?.name ?? '—'}:</strong> {entry.body}
                  {/* Files said with the comment stay with the comment, rather
                      than pooling in the task's list where the sentence that
                      explains them is nowhere in sight. */}
                  <AttachmentList
                    items={attachments.filter((a) => a.commentId === entry.id)}
                    taskId={task.id}
                  />
                </li>
              ))}
            </ul>
            <div style={{ display: 'grid', gap: 'var(--space-2)' }}>
              <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
                <Input
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  placeholder={t('task.writeComment')}
                  aria-label={t('task.writeComment')}
                />
                <Button
                  type="button"
                  loading={addComment.isPending || uploadAttachment.isPending}
                  disabled={!comment.trim()}
                  onClick={sendComment}
                >
                  {t('task.send')}
                </Button>
              </div>
              <CommentAttachmentPicker files={commentFiles} onChange={setCommentFiles} />
            </div>
          </section>
        )}
      </Dialog>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('task.deleteTitle')}
        description={t('task.deleteBody')}
        confirmLabel={t('common.delete')}
        loading={deleteTask.isPending}
        onConfirm={() =>
          task &&
          deleteTask.mutate(task.id, {
            onSuccess: () => {
              toast.success(t('task.deleted'));
              setConfirmDelete(false);
              onClose();
            }
          })
        }
      />
    </>
  );
}

/**
 * Values for the custom fields that apply to this project.
 *
 * Saved as one merge rather than field by field, so a client that knows about
 * three fields cannot erase a fourth it has never heard of.
 */
function CustomFieldsSection({ projectId, task }: { projectId: string; task: Task }) {
  const { t } = useTranslation();
  const { data: definitions = [] } = useCustomFields(projectId);
  const setFields = useSetTaskFields();
  const [draft, setDraft] = useState<Record<string, unknown>>({});

  if (definitions.length === 0) return null;

  const stored = (task as Task & { customFields?: Record<string, unknown> }).customFields ?? {};
  const valueOf = (key: string) => (key in draft ? draft[key] : stored[key]);

  const commit = () => {
    if (Object.keys(draft).length === 0) return;
    setFields.mutate({ taskId: task.id, values: draft }, { onSuccess: () => setDraft({}) });
  };

  return (
    <section>
      <h4 className={controls.label}>{t('task.customFields')}</h4>
      <div style={{ display: 'grid', gap: 'var(--space-3)' }}>
        {definitions.map((definition) => {
          const value = valueOf(definition.key);
          const update = (next: unknown) => setDraft((current) => ({ ...current, [definition.key]: next }));

          return (
            <Field key={definition.id} label={definition.label} required={definition.required}>
              {({ id }) => {
                switch (definition.type) {
                  case 'checkbox':
                    return (
                      <input
                        id={id}
                        type="checkbox"
                        checked={Boolean(value)}
                        onChange={(event) => update(event.target.checked)}
                        onBlur={commit}
                      />
                    );
                  case 'select':
                    return (
                      <SelectField
                        id={id}
                        value={String(value ?? '')}
                        onChange={(next) => {
                          update(next);
                          setFields.mutate({ taskId: task.id, values: { [definition.key]: next } });
                        }}
                        options={definition.options.map((option) => ({ value: option, label: option }))}
                      />
                    );
                  case 'number':
                    return (
                      <Input
                        id={id}
                        type="number"
                        value={String(value ?? '')}
                        onChange={(event) => update(Number(event.target.value))}
                        onBlur={commit}
                      />
                    );
                  case 'date':
                    return (
                      <Input
                        id={id}
                        type="date"
                        value={String(value ?? '')}
                        onChange={(event) => update(event.target.value)}
                        onBlur={commit}
                      />
                    );
                  default:
                    return (
                      <Input
                        id={id}
                        type={definition.type === 'email' ? 'email' : definition.type === 'url' ? 'url' : 'text'}
                        value={String(value ?? '')}
                        onChange={(event) => update(event.target.value)}
                        onBlur={commit}
                      />
                    );
                }
              }}
            </Field>
          );
        })}
      </div>
    </section>
  );
}

/** Timer, manual entries, and estimated against actual. */
function TimeSection({ task }: { task: Task }) {
  const { t } = useTranslation();
  const toast = useToast();
  const { data: entries = [] } = useTaskTime(task.id);
  const { data: running } = useRunningTimer();
  const startTimer = useStartTimer();
  const stopTimer = useStopTimer();
  const logTime = useLogTime();
  const [minutes, setMinutes] = useState('');

  const trackedSeconds = entries.reduce((total, entry) => total + entry.seconds, 0);
  const trackedHours = trackedSeconds / 3600;
  const runningHere = running?.taskId === task.id;
  // Only meaningful once both numbers exist; an estimate of zero is "not
  // estimated", not "estimated at nothing".
  const overBudget = task.estimateHours > 0 && trackedHours > task.estimateHours;

  return (
    <section>
      <h4 className={controls.label}>{t('task.time')}</h4>

      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-3)', flexWrap: 'wrap' }}>
        {runningHere ? (
          <Button variant="danger" size="sm" loading={stopTimer.isPending} onClick={() => stopTimer.mutate()}>
            {t('task.stopTimer')}
          </Button>
        ) : (
          <Button
            variant="secondary"
            size="sm"
            loading={startTimer.isPending}
            onClick={() =>
              startTimer.mutate(task.id, {
                onError: () => toast.error(t('task.timerAlreadyRunning'))
              })
            }
          >
            {t('task.startTimer')}
          </Button>
        )}

        <Badge tone={overBudget ? 'danger' : 'neutral'}>
          {t('task.trackedOfEstimate', {
            tracked: trackedHours.toFixed(1),
            estimate: task.estimateHours || 0
          })}
        </Badge>

        <div style={{ display: 'flex', gap: 'var(--space-2)', marginLeft: 'auto' }}>
          <Input
            type="number"
            min={1}
            value={minutes}
            onChange={(event) => setMinutes(event.target.value)}
            placeholder={t('task.minutes')}
            aria-label={t('task.logTime')}
            style={{ width: 110 }}
          />
          <Button
            size="sm"
            disabled={!minutes || Number(minutes) <= 0}
            loading={logTime.isPending}
            onClick={() =>
              logTime.mutate(
                { taskId: task.id, minutes: Number(minutes) },
                { onSuccess: () => setMinutes('') }
              )
            }
          >
            {t('task.logTime')}
          </Button>
        </div>
      </div>

      {entries.length > 0 && (
        <ul style={{ listStyle: 'none', padding: 0, margin: 'var(--space-3) 0 0', display: 'grid', gap: 4 }}>
          {entries.slice(0, 5).map((entry) => (
            <li key={entry.id} style={{ fontSize: 'var(--text-xs)', color: 'var(--text-secondary)' }}>
              {entry.user?.name ?? '—'} · {(entry.seconds / 3600).toFixed(2)}h
              {!entry.endedAt && ` · ${t('task.running')}`}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function SelectField({
  id,
  value,
  onChange,
  options
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
  return (
    <SelectPrimitive.Root value={value} onValueChange={onChange}>
      <SelectPrimitive.Trigger id={id} className={controls.selectTrigger}>
        <SelectPrimitive.Value />
        <SelectPrimitive.Icon className={controls.selectIcon}>
          <ChevronDown size={15} />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content className={controls.selectContent} position="popper" sideOffset={4}>
          <SelectPrimitive.Viewport className={controls.selectViewport}>
            {options.map((option) => (
              <SelectPrimitive.Item key={option.value} value={option.value} className={clsx(controls.selectItem)}>
                <SelectPrimitive.ItemText>{option.label}</SelectPrimitive.ItemText>
                <SelectPrimitive.ItemIndicator>
                  <Check size={14} />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}
