import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Badge, Card, EmptyState, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { Dialog } from '../ui/Dialog';
import { Field, Input, Textarea } from '../ui/Field';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Folder, Plus } from '../ui/icons';
import { useApplyTemplate, useCreateProject, useProjects, useTemplates } from '../lib/queries';
import { errorMessage } from '../lib/api';
import controls from '../ui/controls.module.css';
import styles from './pages.module.css';

export default function ProjectsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { data: projects, isLoading, isError, refetch } = useProjects();
  const createProject = useCreateProject();

  const { data: templates = [] } = useTemplates('project');
  const applyTemplate = useApplyTemplate();

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: '', key: '', description: '' });
  const [templateId, setTemplateId] = useState('');
  const [error, setError] = useState('');

  /**
   * Creates the project, from a template when one is chosen.
   *
   * Applying a template is a different request rather than a flag on the
   * create call: it creates the status columns and every task the template
   * carries, so it is a different operation that happens to end in a project.
   */
  function submit(event: FormEvent) {
    event.preventDefault();
    setError('');

    const done = () => {
      toast.success(t('projects.created'));
      setForm({ name: '', key: '', description: '' });
      setTemplateId('');
      setOpen(false);
    };
    const failed = (err: unknown) => setError(errorMessage(err, t('errors.genericBody')));

    if (templateId) {
      applyTemplate.mutate(
        { id: templateId, name: form.name, key: form.key },
        { onSuccess: done, onError: failed }
      );
      return;
    }

    createProject.mutate(form, { onSuccess: done, onError: failed });
  }

  const newButton = (
    <Button variant="primary" onClick={() => setOpen(true)}>
      <Plus size={16} />
      {t('projects.new')}
    </Button>
  );

  return (
    <>
      <PageHeader title={t('projects.title')} actions={newButton} />

      {isLoading && <SkeletonList rows={4} height={150} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState
            title={t('errors.loadFailed')}
            body={t('errors.genericBody')}
            onRetry={() => void refetch()}
            retryLabel={t('common.retry')}
          />
        </Card>
      )}

      {projects?.length === 0 && (
        <Card>
          <EmptyState
            icon={<Folder size={22} />}
            title={t('projects.empty')}
            body={t('projects.emptyBody')}
            action={newButton}
          />
        </Card>
      )}

      {projects && projects.length > 0 && (
        <ul className={`${styles.grid} ${styles.gridCards}`} style={{ listStyle: 'none', padding: 0 }}>
          {projects.map((project) => (
            <Card as="li" key={project.id} interactive padded={false}>
              <Link to={`/projects/${project.id}`} className={styles.projectCard} style={{ padding: 'var(--space-5)' }}>
                <span className={styles.projectSwatch} style={{ background: project.color }} aria-hidden="true" />
                <span className={styles.projectName}>{project.name}</span>
                <span className={styles.subtle}>{project.key}</span>
                {project.description && <span className={styles.muted}>{project.description}</span>}
                <span className={styles.projectMeta}>
                  <Badge>{project.status}</Badge>
                  <span className={styles.subtle}>
                    {t('projects.memberCount', { count: project.members?.length ?? 0 })}
                  </span>
                </span>
              </Link>
            </Card>
          ))}
        </ul>
      )}

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('projects.new')}
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="primary"
              type="submit"
              form="new-project"
              loading={createProject.isPending || applyTemplate.isPending}
            >
              {t('common.create')}
            </Button>
          </>
        }
      >
        <form id="new-project" onSubmit={submit} style={{ display: 'grid', gap: 'var(--space-4)' }}>
          <Field label={t('projects.name')} required>
            {({ id }) => (
              <Input
                id={id}
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                required
                autoFocus
              />
            )}
          </Field>

          <Field label={t('projects.key')} hint={t('projects.keyHint')} required>
            {({ id, describedBy }) => (
              <Input
                id={id}
                aria-describedby={describedBy}
                value={form.key}
                onChange={(e) => setForm((f) => ({ ...f, key: e.target.value.toUpperCase() }))}
                required
              />
            )}
          </Field>

          <Field label={t('projects.description')}>
            {({ id }) => (
              <Textarea
                id={id}
                value={form.description}
                onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
              />
            )}
          </Field>

          {templates.length > 0 && (
            <fieldset style={{ border: 'none', padding: 0, margin: 0 }}>
              <legend className={controls.label} style={{ marginBottom: 'var(--space-2)' }}>
                {t('templates.startFrom')}
              </legend>
              <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
                <Button
                  type="button"
                  size="sm"
                  variant={templateId === '' ? 'primary' : 'secondary'}
                  aria-pressed={templateId === ''}
                  onClick={() => setTemplateId('')}
                >
                  {t('templates.blank')}
                </Button>
                {templates.map((template) => (
                  <Button
                    key={template.id}
                    type="button"
                    size="sm"
                    variant={templateId === template.id ? 'primary' : 'secondary'}
                    aria-pressed={templateId === template.id}
                    onClick={() => setTemplateId(template.id)}
                  >
                    {template.name}
                  </Button>
                ))}
              </div>
              {templateId && (
                <p className={controls.hint} style={{ marginTop: 'var(--space-2)' }}>
                  {t('templates.willCreate')}
                </p>
              )}
            </fieldset>
          )}

          {error && (
            <p role="alert" style={{ color: 'var(--danger)', fontSize: 'var(--text-sm)' }}>
              {error}
            </p>
          )}
        </form>
      </Dialog>
    </>
  );
}
