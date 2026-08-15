import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import * as SelectPrimitive from '@radix-ui/react-select';
import clsx from 'clsx';
import { Dialog, ConfirmDialog } from '../ui/Dialog';
import { Button } from '../ui/Button';
import { Field, Input, Textarea } from '../ui/Field';
import { Avatar, Badge } from '../ui/display';
import { useToast } from '../ui/Toast';
import { Check, ChevronDown } from '../ui/icons';
import { useAddComment, useDeleteTask, useSaveTask, useTask } from '../lib/queries';
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

        {isEdit && (
          <section>
            <h4 className={controls.label}>{t('task.comments')}</h4>
            <ul style={{ listStyle: 'none', padding: 0, margin: '0 0 var(--space-3)', display: 'grid', gap: 'var(--space-2)' }}>
              {current?.comments?.map((entry) => (
                <li key={entry.id} style={{ fontSize: 'var(--text-sm)' }}>
                  <strong>{entry.author?.name ?? '—'}:</strong> {entry.body}
                </li>
              ))}
            </ul>
            <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
              <Input
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder={t('task.writeComment')}
                aria-label={t('task.writeComment')}
              />
              <Button
                type="button"
                loading={addComment.isPending}
                disabled={!comment.trim()}
                onClick={() =>
                  task &&
                  addComment.mutate(
                    { taskId: task.id, body: comment },
                    { onSuccess: () => setComment('') }
                  )
                }
              >
                {t('task.send')}
              </Button>
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
