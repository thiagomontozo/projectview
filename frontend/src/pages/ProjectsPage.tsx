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
import { useCreateProject, useProjects } from '../lib/queries';
import { errorMessage } from '../lib/api';
import styles from './pages.module.css';

export default function ProjectsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { data: projects, isLoading, isError, refetch } = useProjects();
  const createProject = useCreateProject();

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: '', key: '', description: '' });
  const [error, setError] = useState('');

  function submit(event: FormEvent) {
    event.preventDefault();
    setError('');
    createProject.mutate(form, {
      onSuccess: () => {
        toast.success(t('projects.created'));
        setForm({ name: '', key: '', description: '' });
        setOpen(false);
      },
      onError: (err) => setError(errorMessage(err, t('errors.genericBody')))
    });
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
            <Button variant="primary" type="submit" form="new-project" loading={createProject.isPending}>
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
