import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Avatar, AvatarGroup, Card, EmptyState, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { Dialog } from '../ui/Dialog';
import { Field, Input, Textarea } from '../ui/Field';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Plus, Users } from '../ui/icons';
import { useCreateTeam, useTeams } from '../lib/queries';
import TeamMembersDialog from '../components/TeamMembersDialog';
import type { Team } from '../types';
import { errorMessage } from '../lib/api';
import styles from './pages.module.css';

export default function TeamsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { data: teams, isLoading, isError, refetch } = useTeams();
  const createTeam = useCreateTeam();

  const [open, setOpen] = useState(false);
  const [managing, setManaging] = useState<Team | null>(null);
  const [form, setForm] = useState({ name: '', description: '' });
  const [error, setError] = useState('');

  function submit(event: FormEvent) {
    event.preventDefault();
    setError('');
    createTeam.mutate(form, {
      onSuccess: () => {
        toast.success(t('teams.created'));
        setForm({ name: '', description: '' });
        setOpen(false);
      },
      onError: (err) => setError(errorMessage(err, t('errors.genericBody')))
    });
  }

  const newButton = (
    <Button variant="primary" onClick={() => setOpen(true)}>
      <Plus size={16} />
      {t('teams.new')}
    </Button>
  );

  return (
    <>
      <PageHeader title={t('teams.title')} actions={newButton} />

      {isLoading && <SkeletonList rows={3} height={130} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState title={t('errors.loadFailed')} onRetry={() => void refetch()} retryLabel={t('common.retry')} />
        </Card>
      )}

      {teams?.length === 0 && (
        <Card>
          <EmptyState icon={<Users size={22} />} title={t('teams.empty')} body={t('teams.emptyBody')} action={newButton} />
        </Card>
      )}

      {teams && teams.length > 0 && (
        <ul className={`${styles.grid} ${styles.gridCards}`} style={{ listStyle: 'none', padding: 0 }}>
          {teams.map((team) => (
            <Card as="li" key={team.id}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)', marginBottom: 'var(--space-2)' }}>
                <span
                  style={{ width: 10, height: 10, borderRadius: '50%', background: team.color, flexShrink: 0 }}
                  aria-hidden="true"
                />
                <span className={styles.projectName}>{team.name}</span>
              </div>
              {team.description && <p className={styles.muted}>{team.description}</p>}
              <div className={styles.projectMeta}>
                <AvatarGroup>
                  {(team.members ?? []).slice(0, 5).map((member) => (
                    <Avatar key={member.id} name={member.name} color={member.avatarColor} size={24} />
                  ))}
                </AvatarGroup>
                <span className={styles.subtle}>
                  {t('projects.memberCount', { count: team.members?.length ?? 0 })}
                </span>
                {/* The endpoints to add and remove members existed from the
                    start; nothing in the interface called them, so a team could
                    be created and never staffed. */}
                <Button
                  variant="secondary"
                  size="sm"
                  style={{ marginLeft: 'auto' }}
                  onClick={() => setManaging(team)}
                >
                  {t('teams.manage')}
                </Button>
              </div>
            </Card>
          ))}
        </ul>
      )}

      {managing && (
        <TeamMembersDialog
          team={teams?.find((candidate) => candidate.id === managing.id) ?? managing}
          onClose={() => setManaging(null)}
        />
      )}

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('teams.new')}
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" type="submit" form="new-team" loading={createTeam.isPending}>
              {t('common.create')}
            </Button>
          </>
        }
      >
        <form id="new-team" onSubmit={submit} style={{ display: 'grid', gap: 'var(--space-4)' }}>
          <Field label={t('teams.name')} required>
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
          <Field label={t('teams.description')}>
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
