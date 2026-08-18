import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Dialog } from '../ui/Dialog';
import { Button } from '../ui/Button';
import { Input, Textarea } from '../ui/Field';
import { Badge, EmptyState } from '../ui/display';
import { Spinner } from '../ui/Spinner';
import { useToast } from '../ui/Toast';
import { Check, Inbox, Plus, Trash } from '../ui/icons';
import { errorMessage } from '../lib/api';
import {
  useAcceptSuggestion,
  useCreateIntakeForm,
  useDeleteIntakeForm,
  useIntakeForms,
  useIntakeSubmissions,
  useSetIntakeFormEnabled
} from '../lib/queries';
import { formatDateTime } from '../lib/format';
import controls from '../ui/controls.module.css';
import styles from './intake.module.css';
import type { IntakeField, IntakeForm, IntakeSubmission } from '../types';

/**
 * Intake forms for a project, and what has come in through them.
 *
 * Two halves of one job, which is why they share a dialog rather than sitting
 * on separate screens: the forms are configured rarely and read constantly, and
 * somebody checking what arrived should not have to remember where the form
 * that produced it lives.
 *
 * A submission is already a task by the time it appears here — that is the
 * intake design, not a shortcut. What this screen adds is the model's
 * suggestion about it, which is stored beside the submission and applied only
 * when somebody presses the button. The model proposes; a person decides.
 */
export default function IntakeDialog({
  projectId,
  onClose
}: {
  projectId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const forms = useIntakeForms(projectId);
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const active = useMemo(
    () => forms.data?.find((form) => form.id === selected) ?? forms.data?.[0],
    [forms.data, selected]
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()} title={t('intake.title')} size="lg">
      <p className={styles.hint}>
        {t('intake.hint')}
      </p>

      {forms.isLoading ? (
        <Spinner />
      ) : creating ? (
        <NewForm
          projectId={projectId}
          onDone={(form) => {
            setCreating(false);
            if (form) setSelected(form.id);
          }}
        />
      ) : !forms.data?.length ? (
        <EmptyState
          icon={<Inbox size={28} />}
          title={t('intake.noForms')}
          body={t('intake.noFormsBody')}
          action={
            <Button variant="primary" onClick={() => setCreating(true)}>
              <Plus size={16} />
              {t('intake.newForm')}
            </Button>
          }
        />
      ) : (
        <>
          <div className={styles.tabs}>
            {forms.data.map((form) => (
              <Button
                key={form.id}
                size="sm"
                variant={form.id === active?.id ? 'primary' : 'ghost'}
                onClick={() => setSelected(form.id)}
              >
                {form.title}
              </Button>
            ))}
            <Button size="sm" variant="ghost" onClick={() => setCreating(true)}>
              <Plus size={16} />
              {t('intake.newForm')}
            </Button>
          </div>

          {active && <FormPanel projectId={projectId} form={active} />}
        </>
      )}
    </Dialog>
  );
}

function FormPanel({ projectId, form }: { projectId: string; form: IntakeForm }) {
  const { t } = useTranslation();
  const toast = useToast();
  const submissions = useIntakeSubmissions(form.id);
  const setEnabled = useSetIntakeFormEnabled(projectId);
  const remove = useDeleteIntakeForm(projectId);

  // The address is the secret — the slug is 128 bits from crypto/rand, not a
  // name — so it is shown in full for copying rather than abbreviated.
  const publicUrl = `${window.location.origin}/intake/${form.slug}`;

  return (
    <div className={styles.panel}>
      <div className={styles.row}>
        <Badge tone={form.enabled ? 'success' : undefined}>
          {form.enabled ? t('intake.open') : t('intake.closed')}
        </Badge>
        {form.public && <Badge tone="accent">{t('intake.publicForm')}</Badge>}
        <Button
          size="sm"
          variant="ghost"
          loading={setEnabled.isPending}
          onClick={() =>
            setEnabled.mutate(
              { id: form.id, enabled: !form.enabled },
              { onError: (error) => toast.error(errorMessage(error, t('errors.genericBody'))) }
            )
          }
        >
          {form.enabled ? t('intake.close') : t('intake.reopen')}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          loading={remove.isPending}
          onClick={() => {
            if (!window.confirm(t('intake.confirmDelete'))) return;
            remove.mutate(form.id, { onError: (error) => toast.error(errorMessage(error, t('errors.genericBody'))) });
          }}
        >
          <Trash size={16} />
          {t('common.delete')}
        </Button>
      </div>

      {form.public && (
        <label className={styles.panel}>
          {t('intake.publicAddress')}
          <Input readOnly value={publicUrl} onFocus={(event) => event.currentTarget.select()} />
          <span className={styles.muted}>{t('intake.publicAddressHint')}</span>
        </label>
      )}

      <h3>{t('intake.submissions')}</h3>
      {submissions.isLoading ? (
        <Spinner />
      ) : !submissions.data?.length ? (
        <p className={styles.muted}>{t('intake.noSubmissions')}</p>
      ) : (
        <ul className={styles.submissions}>
          {submissions.data.map((submission) => (
            <SubmissionRow
              key={submission.id}
              projectId={projectId}
              form={form}
              submission={submission}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function SubmissionRow({
  projectId,
  form,
  submission
}: {
  projectId: string;
  form: IntakeForm;
  submission: IntakeSubmission;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const accept = useAcceptSuggestion(projectId, form.id);
  const suggestion = submission.suggestion;

  // Something worth applying, as opposed to a suggestion the model returned
  // empty — which is a normal answer for a submission that says nothing useful.
  const hasSomething = Boolean(suggestion?.priority || suggestion?.assigneeId);

  return (
    <li className={styles.entry}>
      <div className={styles.entryHead}>
        <strong>{submission.submitterName || t('intake.anonymous')}</strong>
        <span className={styles.muted}>{formatDateTime(submission.createdAt)}</span>
      </div>
      {submission.submitterEmail && <div className={styles.muted}>{submission.submitterEmail}</div>}

      <dl className={styles.answers}>
        {form.fields.map((field: IntakeField) => {
          const answer = submission.answers?.[field.key];
          if (answer === undefined || answer === null || answer === '') return null;
          return (
            <div key={field.key} className={styles.answer}>
              <dt>{field.label}</dt>
              <dd>{String(answer)}</dd>
            </div>
          );
        })}
      </dl>

      {/* The suggestion is labelled as a suggestion wherever it appears. A row
          that quietly showed a priority would be indistinguishable from one a
          person chose, and the whole point of storing it separately is that the
          two stay distinguishable. */}
      {suggestion && hasSomething && (
        <div className={styles.suggestion}>
          <div className={styles.row}>
            <Badge tone="accent">{t('intake.suggested')}</Badge>
            {suggestion.priority && <Badge>{t(`task.priority${suggestion.priority.charAt(0).toUpperCase()}${suggestion.priority.slice(1)}`)}</Badge>}
            {suggestion.assigneeName && <Badge>{suggestion.assigneeName}</Badge>}
            {submission.acceptedAt ? (
              <span className={styles.muted}>
                <Check size={14} /> {t('intake.accepted', { at: formatDateTime(submission.acceptedAt) })}
              </span>
            ) : (
              <Button
                size="sm"
                variant="secondary"
                loading={accept.isPending}
                disabled={!submission.taskId}
                onClick={() =>
                  accept.mutate(submission.id, {
                    onSuccess: () => toast.success(t('intake.acceptedNow')),
                    onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
                  })
                }
              >
                {t('intake.accept')}
              </Button>
            )}
          </div>
          {suggestion.summary && <p className={styles.summary}>{suggestion.summary}</p>}
          {suggestion.model && (
            <span className={styles.muted}>{t('intake.byModel', { model: suggestion.model })}</span>
          )}
        </div>
      )}
    </li>
  );
}

/**
 * A new form.
 *
 * Fields are added one at a time with a type, because the type decides what the
 * public page renders and what the server will accept. The key is derived from
 * the label rather than asked for: it is a storage detail, and asking somebody
 * building a form to invent a stable identifier is asking them to make a
 * mistake they will only discover later.
 */
function NewForm({
  projectId,
  onDone
}: {
  projectId: string;
  onDone: (form?: IntakeForm) => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const create = useCreateIntakeForm(projectId);

  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [isPublic, setIsPublic] = useState(false);
  const [fields, setFields] = useState<IntakeField[]>([
    { key: 'summary', label: t('intake.defaultSummary'), type: 'text', required: true },
    { key: 'details', label: t('intake.defaultDetails'), type: 'textarea', required: false }
  ]);

  function addField() {
    setFields((current) => [
      ...current,
      { key: `field_${current.length + 1}`, label: '', type: 'text', required: false }
    ]);
  }

  function update(index: number, patch: Partial<IntakeField>) {
    setFields((current) =>
      current.map((field, i) => {
        if (i !== index) return field;
        const next = { ...field, ...patch };
        if (patch.label !== undefined) next.key = slugify(patch.label) || `field_${i + 1}`;
        return next;
      })
    );
  }

  return (
    <div className={styles.form}>
      <label>
        {t('intake.formTitle')}
        <Input value={title} onChange={(event) => setTitle(event.target.value)} />
      </label>
      <label>
        {t('common.description')}
        <Textarea rows={2} value={description} onChange={(event) => setDescription(event.target.value)} />
      </label>
      <label className={styles.row}>
        <input type="checkbox" checked={isPublic} onChange={(event) => setIsPublic(event.target.checked)} />
        {t('intake.allowAnonymous')}
        <span className={styles.muted}>{t('intake.allowAnonymousHint')}</span>
      </label>

      <div>
        <strong>{t('intake.fields')}</strong>
        {fields.map((field, index) => (
          <div key={index} className={styles.field}>
            <Input
              placeholder={t('intake.fieldLabel')}
              value={field.label}
              onChange={(event) => update(index, { label: event.target.value })}
            />
            <select
              className={controls.input}
              value={field.type}
              onChange={(event) => update(index, { type: event.target.value as IntakeField['type'] })}
            >
              {(['text', 'textarea', 'number', 'date', 'select', 'checkbox', 'email'] as const).map((type) => (
                <option key={type} value={type}>
                  {t(`intake.type_${type}`)}
                </option>
              ))}
            </select>
            <label className={styles.checkbox}>
              <input
                type="checkbox"
                checked={field.required}
                onChange={(event) => update(index, { required: event.target.checked })}
              />
              {t('intake.required')}
            </label>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setFields((current) => current.filter((_, i) => i !== index))}
            >
              <Trash size={16} />
            </Button>
          </div>
        ))}
        <Button size="sm" variant="ghost" onClick={addField}>
          <Plus size={16} />
          {t('intake.addField')}
        </Button>
      </div>

      <div className={styles.actions}>
        <Button variant="ghost" onClick={() => onDone()}>
          {t('common.cancel')}
        </Button>
        <Button
          variant="primary"
          loading={create.isPending}
          disabled={!title.trim() || fields.some((field) => !field.label.trim())}
          onClick={() =>
            create.mutate(
              {
                title: title.trim(),
                description: description.trim(),
                fields,
                public: isPublic
              },
              {
                onSuccess: (form) => onDone(form),
                onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
              }
            )
          }
        >
          {t('common.create')}
        </Button>
      </div>
    </div>
  );
}

function slugify(label: string): string {
  return label
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .slice(0, 40);
}
