import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import * as SwitchPrimitive from '@radix-ui/react-switch';
import { PageHeader } from '../app/AppShell';
import { Avatar, AvatarGroup, Badge, Card, EmptyState, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { Dialog } from '../ui/Dialog';
import { Field, Input, Textarea } from '../ui/Field';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Layers, Plus } from '../ui/icons';
import { useCreateSpace, useSpaces } from '../lib/queries';
import { errorMessage } from '../lib/api';
import controls from '../ui/controls.module.css';
import styles from './pages.module.css';

/**
 * Spaces are the top of the hierarchy (Space → Folder → List → Task). This
 * screen is what makes the structure — and the role a person holds on it —
 * visible, rather than a capability that only exists in the API.
 */
export default function SpacesPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { data: spaces, isLoading, isError, refetch } = useSpaces();
  const createSpace = useCreateSpace();

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: '', description: '', isPrivate: false });
  const [error, setError] = useState('');

  function submit(event: FormEvent) {
    event.preventDefault();
    setError('');
    createSpace.mutate(form, {
      onSuccess: () => {
        toast.success(t('spaces.created'));
        setForm({ name: '', description: '', isPrivate: false });
        setOpen(false);
      },
      onError: (err) => setError(errorMessage(err, t('errors.genericBody')))
    });
  }

  const newButton = (
    <Button variant="primary" onClick={() => setOpen(true)}>
      <Plus size={16} />
      {t('spaces.new')}
    </Button>
  );

  const roleLabel = (role?: string) => {
    switch (role) {
      case 'owner':
        return t('spaces.roleOwner');
      case 'admin':
        return t('spaces.roleAdmin');
      case 'member':
        return t('spaces.roleMember');
      case 'guest':
        return t('spaces.roleGuest');
      default:
        return null;
    }
  };

  return (
    <>
      <PageHeader title={t('spaces.title')} actions={newButton} />

      {isLoading && <SkeletonList rows={3} height={130} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState
            title={t('errors.loadFailed')}
            onRetry={() => void refetch()}
            retryLabel={t('common.retry')}
          />
        </Card>
      )}

      {spaces?.length === 0 && (
        <Card>
          <EmptyState
            icon={<Layers size={22} />}
            title={t('spaces.empty')}
            body={t('spaces.emptyBody')}
            action={newButton}
          />
        </Card>
      )}

      {spaces && spaces.length > 0 && (
        <ul className={`${styles.grid} ${styles.gridCards}`} style={{ listStyle: 'none', padding: 0 }}>
          {spaces.map((space) => (
            <Card as="li" key={space.id}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)', marginBottom: 'var(--space-2)' }}>
                <span
                  style={{
                    width: 10,
                    height: 10,
                    borderRadius: '50%',
                    background: space.color,
                    flexShrink: 0
                  }}
                  aria-hidden="true"
                />
                <span className={styles.projectName}>{space.name}</span>
                {space.isPrivate && <Badge tone="warning">{t('spaces.private')}</Badge>}
              </div>

              {space.description && <p className={styles.muted}>{space.description}</p>}

              <div className={styles.projectMeta}>
                <AvatarGroup>
                  {space.members.slice(0, 4).map(({ user }) => (
                    <Avatar key={user.id} name={user.name} color={user.avatarColor} size={24} />
                  ))}
                </AvatarGroup>
                {space.yourRole && <Badge tone="accent">{roleLabel(space.yourRole)}</Badge>}
              </div>
            </Card>
          ))}
        </ul>
      )}

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('spaces.new')}
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" type="submit" form="new-space" loading={createSpace.isPending}>
              {t('common.create')}
            </Button>
          </>
        }
      >
        <form id="new-space" onSubmit={submit} style={{ display: 'grid', gap: 'var(--space-4)' }}>
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

          <Field label={t('projects.description')}>
            {({ id }) => (
              <Textarea
                id={id}
                value={form.description}
                onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
              />
            )}
          </Field>

          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 'var(--space-4)' }}>
            <label htmlFor="space-private" style={{ fontSize: 'var(--text-base)' }}>
              {t('spaces.private')}
              <span className={styles.subtle} style={{ display: 'block' }}>
                {t('spaces.privateHint')}
              </span>
            </label>
            <SwitchPrimitive.Root
              id="space-private"
              className={controls.switchRoot}
              checked={form.isPrivate}
              onCheckedChange={(checked) => setForm((f) => ({ ...f, isPrivate: checked }))}
            >
              <SwitchPrimitive.Thumb className={controls.switchThumb} />
            </SwitchPrimitive.Root>
          </div>

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
