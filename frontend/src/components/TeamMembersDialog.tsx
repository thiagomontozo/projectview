import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Dialog } from '../ui/Dialog';
import { Button } from '../ui/Button';
import { Input } from '../ui/Field';
import { Avatar, Badge } from '../ui/display';
import { useToast } from '../ui/Toast';
import { Search, Trash } from '../ui/icons';
import { errorMessage } from '../lib/api';
import {
  MIN_DIRECTORY_QUERY,
  useAddTeamMember,
  useDirectorySearch,
  useRemoveTeamMember,
  useUsers
} from '../lib/queries';
import controls from '../ui/controls.module.css';
import type { Team } from '../types';

/**
 * Who is on a team, and how somebody gets added.
 *
 * One search box over two sources. People who already have an account here are
 * matched locally as you type; Active Directory is searched in parallel for
 * everybody else, so a colleague can be put on a team **before they have ever
 * signed in** — which was impossible before, because just-in-time provisioning
 * meant an account only appeared at first login.
 *
 * A directory result that has no account here is labelled as such, because
 * picking it does two things rather than one: it creates the account and then
 * allocates the person. Somebody choosing from a list deserves to know which of
 * the two they are about to do.
 */
export default function TeamMembersDialog({ team, onClose }: { team: Team; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();

  const [query, setQuery] = useState('');
  const { data: allUsers = [] } = useUsers();
  const directory = useDirectorySearch(query);
  const addMember = useAddTeamMember();
  const removeMember = useRemoveTeamMember();

  const memberIds = useMemo(() => new Set((team.members ?? []).map((m) => m.id)), [team.members]);

  // Local matches first: somebody who already has an account is the common
  // case, and making them wait behind a directory round trip would be slower
  // for the thing that happens most.
  const localMatches = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return [];
    return allUsers
      .filter((user) => !memberIds.has(user.id))
      .filter(
        (user) =>
          user.name.toLowerCase().includes(needle) ||
          user.email.toLowerCase().includes(needle) ||
          user.username.toLowerCase().includes(needle)
      )
      .slice(0, 8);
  }, [allUsers, query, memberIds]);

  // Directory people who are not already an account and not already on the
  // team — anyone else is shown by the local list above.
  const directoryMatches = useMemo(
    () => (directory.data?.results ?? []).filter((entry) => !entry.known),
    [directory.data]
  );

  function add(body: { userId?: string; directoryUsername?: string }, label: string) {
    addMember.mutate(
      { teamId: team.id, ...body },
      {
        onSuccess: () => {
          toast.success(t('teams.memberAdded', { name: label }));
          setQuery('');
        },
        onError: (err) => toast.error(errorMessage(err, t('errors.genericBody')))
      }
    );
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => !open && onClose()}
      size="lg"
      title={t('teams.manageMembers', { team: team.name })}
      footer={
        <Button variant="ghost" onClick={onClose} style={{ marginLeft: 'auto' }}>
          {t('common.close')}
        </Button>
      }
    >
      <div style={{ display: 'grid', gap: 'var(--space-4)' }}>
        <section>
          <h4 className={controls.label}>{t('teams.currentMembers')}</h4>
          {(team.members ?? []).length === 0 ? (
            <p className={controls.hint}>{t('teams.noMembers')}</p>
          ) : (
            <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 'var(--space-2)' }}>
              {(team.members ?? []).map((member) => (
                <li
                  key={member.id}
                  style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}
                >
                  <Avatar name={member.name} color={member.avatarColor} size={24} />
                  <span style={{ fontSize: 'var(--text-sm)' }}>{member.name}</span>
                  <span className={controls.hint}>{member.email}</span>
                  <Button
                    variant="dangerGhost"
                    size="sm"
                    style={{ marginLeft: 'auto' }}
                    aria-label={t('teams.removeMember', { name: member.name })}
                    loading={removeMember.isPending}
                    onClick={() =>
                      removeMember.mutate(
                        { teamId: team.id, userId: member.id },
                        { onError: (err) => toast.error(errorMessage(err, t('errors.genericBody'))) }
                      )
                    }
                  >
                    <Trash size={15} />
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section>
          <h4 className={controls.label}>{t('teams.addMember')}</h4>
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('teams.searchPeople')}
            aria-label={t('teams.searchPeople')}
            icon={<Search size={15} />}
          />

          {query.trim().length > 0 && query.trim().length < MIN_DIRECTORY_QUERY && (
            <p className={controls.hint} style={{ marginTop: 'var(--space-2)' }}>
              {t('teams.keepTyping')}
            </p>
          )}

          <ul style={{ listStyle: 'none', padding: 0, margin: 'var(--space-2) 0 0', display: 'grid', gap: 'var(--space-1)' }}>
            {localMatches.map((user) => (
              <li key={user.id}>
                <Button
                  variant="ghost"
                  block
                  style={{ justifyContent: 'flex-start' }}
                  loading={addMember.isPending}
                  onClick={() => add({ userId: user.id }, user.name)}
                >
                  <Avatar name={user.name} color={user.avatarColor} size={20} />
                  {user.name}
                  <span className={controls.hint} style={{ marginLeft: 'var(--space-2)' }}>{user.email}</span>
                </Button>
              </li>
            ))}

            {directoryMatches.map((entry) => (
              <li key={entry.username}>
                <Button
                  variant="ghost"
                  block
                  style={{ justifyContent: 'flex-start' }}
                  loading={addMember.isPending}
                  onClick={() => add({ directoryUsername: entry.username }, entry.name)}
                >
                  <Avatar name={entry.name} size={20} />
                  {entry.name}
                  <span className={controls.hint} style={{ marginLeft: 'var(--space-2)' }}>{entry.email}</span>
                  {/* Says what picking this will do: create the account, then
                      allocate. Without it the two rows look identical. */}
                  <span style={{ marginLeft: 'auto' }}>
                    <Badge tone="accent">{t('teams.fromDirectory')}</Badge>
                  </span>
                </Button>
              </li>
            ))}
          </ul>

          {/* "Nobody matched" and "the directory could not be consulted" must
              not look the same, so the reason is shown rather than an empty
              list that implies the person does not exist. */}
          {directory.data && !directory.data.searched && query.trim().length >= MIN_DIRECTORY_QUERY && (
            <p className={controls.hint} style={{ marginTop: 'var(--space-2)' }}>
              {t('teams.directoryUnavailable')}
            </p>
          )}

          {query.trim().length >= MIN_DIRECTORY_QUERY &&
            !directory.isFetching &&
            localMatches.length === 0 &&
            directoryMatches.length === 0 && (
              <p className={controls.hint} style={{ marginTop: 'var(--space-2)' }}>
                {t('teams.nobodyFound')}
              </p>
            )}

          <p className={controls.hint} style={{ marginTop: 'var(--space-3)' }}>
            {t('teams.createLocallyHint')}
          </p>
        </section>
      </div>
    </Dialog>
  );
}
