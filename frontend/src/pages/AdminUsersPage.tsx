import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Avatar, Badge, Card, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { Dialog } from '../ui/Dialog';
import { Field, Input } from '../ui/Field';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Plus, Users } from '../ui/icons';
import controls from '../ui/controls.module.css';
import { useAuth } from '../lib/auth';
import { errorMessage } from '../lib/api';
import { useUsers } from '../lib/queries';
import {
  useCreateUser,
  useResetPassword,
  useSetUserActive,
  useSetUserRole,
  type NewUser
} from '../lib/enterprise';
import { formatDate } from '../lib/format';
import styles from './pages.module.css';
import type { Role, User } from '../types';

const ROLES: Role[] = ['admin', 'manager', 'member'];

const ROLE_TONES: Record<Role, 'accent' | 'warning' | 'neutral'> = {
  admin: 'accent',
  manager: 'warning',
  member: 'neutral'
};

/**
 * Accounts, and who may do what with them.
 *
 * The API has always allowed this — promoting somebody, deactivating an
 * account, resetting a password are all authorised and audited — but until now
 * there was no screen, so adding a colleague meant reaching for curl.
 *
 * The server refuses the routes for anyone who is not an administrator. Hiding
 * the screen is only so nobody else finds a page they cannot use.
 */
export default function AdminUsersPage() {
  const { t, i18n } = useTranslation();
  const { user: me } = useAuth();
  const isAdmin = me?.role === 'admin';

  const { data: users, isLoading, isError, refetch } = useUsers();
  const [creating, setCreating] = useState(false);
  const [search, setSearch] = useState('');

  const visible = useMemo(() => {
    const needle = search.trim().toLowerCase();
    const rows = users ?? [];
    const matched = needle
      ? rows.filter((u) =>
          [u.name, u.username, u.email].some((field) => field?.toLowerCase().includes(needle))
        )
      : rows;
    // Administrators first, then by name: the people who can change things are
    // who an administrator opens this page to look at.
    return [...matched].sort((a, b) => {
      if (a.role !== b.role) return ROLES.indexOf(a.role) - ROLES.indexOf(b.role);
      return a.name.localeCompare(b.name);
    });
  }, [users, search]);

  const activeAdmins = (users ?? []).filter((u) => u.role === 'admin' && u.active).length;

  if (!isAdmin) {
    return (
      <>
        <PageHeader title={t('users.title')} />
        <Card>
          <ErrorState title={t('admin.restricted')} body={t('users.restrictedBody')} />
        </Card>
      </>
    );
  }

  return (
    <>
      <PageHeader
        title={t('users.title')}
        description={t('users.hint')}
        actions={
          <Button variant="primary" onClick={() => setCreating(true)}>
            <Plus size={16} />
            {t('users.new')}
          </Button>
        }
      />

      {/* An installation with one administrator is one careless click from
          having none. The server refuses that click; this says so first. */}
      {activeAdmins === 1 && (
        <Card>
          <p className={styles.muted}>{t('users.singleAdminWarning')}</p>
        </Card>
      )}

      <div className={styles.filterRow}>
        <input
          className={controls.input}
          type="search"
          placeholder={t('users.search')}
          aria-label={t('users.search')}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
      </div>

      {isLoading && <SkeletonList rows={5} height={52} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState title={t('errors.loadFailed')} onRetry={() => void refetch()} retryLabel={t('common.retry')} />
        </Card>
      )}

      {users && visible.length === 0 && (
        <Card>
          <ErrorState title={t('users.noMatches')} />
        </Card>
      )}

      {visible.length > 0 && (
        <Card padded={false}>
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th scope="col">{t('users.person')}</th>
                  <th scope="col">{t('users.role')}</th>
                  <th scope="col">{t('users.status')}</th>
                  <th scope="col">{t('users.lastLogin')}</th>
                  <th scope="col">{t('users.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((person) => (
                  <UserRow
                    key={person.id}
                    person={person}
                    isSelf={person.id === me?.id}
                    locale={i18n.language}
                  />
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <NewUserDialog open={creating} onClose={() => setCreating(false)} />
    </>
  );
}

function UserRow({ person, isSelf, locale }: { person: User; isSelf: boolean; locale: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const setRole = useSetUserRole();
  const setActive = useSetUserActive();
  const [resetting, setResetting] = useState(false);

  // The server is the authority on whether a change is allowed — it refuses
  // one that would leave nobody able to administer, with 409 and a message
  // written for this. Surfacing that message beats guessing here and being
  // wrong about a list the browser may not have refreshed.
  const report = (error: unknown) => toast.error(errorMessage(error, t('errors.genericBody')));

  return (
    <tr>
      <td>
        <div className={styles.personCell}>
          <Avatar name={person.name} color={person.avatarColor} size={30} />
          <div>
            <div style={{ fontWeight: 'var(--weight-medium)' }}>
              {person.name}
              {isSelf && <span className={styles.subtle}> · {t('users.you')}</span>}
            </div>
            <div className={styles.subtle}>
              {person.username} · {person.email}
            </div>
          </div>
        </div>
      </td>

      <td>
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
          <Badge tone={ROLE_TONES[person.role]}>{t(`users.role_${person.role}`)}</Badge>
          <select
            className={controls.input}
            style={{ width: 130 }}
            aria-label={`${t('users.role')} — ${person.name}`}
            value={person.role}
            disabled={setRole.isPending}
            onChange={(event) =>
              setRole.mutate(
                { id: person.id, role: event.target.value as Role },
                {
                  onSuccess: (updated) =>
                    toast.success(t('users.roleChanged', { name: updated.name, role: t(`users.role_${updated.role}`) })),
                  onError: report
                }
              )
            }
          >
            {ROLES.map((role) => (
              <option key={role} value={role}>
                {t(`users.role_${role}`)}
              </option>
            ))}
          </select>
        </div>
      </td>

      <td>
        <Badge tone={person.active ? 'success' : 'neutral'}>
          {person.active ? t('users.active') : t('users.inactive')}
        </Badge>
      </td>

      <td className={styles.subtle}>{formatDate(person.lastLoginAt, locale) || t('users.never')}</td>

      <td>
        <div style={{ display: 'flex', gap: 'var(--space-2)', justifyContent: 'flex-end' }}>
          <Button size="sm" variant="ghost" onClick={() => setResetting(true)}>
            {t('users.resetPassword')}
          </Button>
          <Button
            size="sm"
            variant={person.active ? 'dangerGhost' : 'secondary'}
            loading={setActive.isPending}
            onClick={() =>
              setActive.mutate(
                { id: person.id, active: !person.active },
                {
                  onSuccess: () =>
                    toast.success(person.active ? t('users.deactivated') : t('users.reactivated')),
                  onError: report
                }
              )
            }
          >
            {person.active ? t('users.deactivate') : t('users.reactivate')}
          </Button>
        </div>

        <ResetPasswordDialog
          open={resetting}
          person={person}
          isSelf={isSelf}
          onClose={() => setResetting(false)}
        />
      </td>
    </tr>
  );
}

function ResetPasswordDialog({
  open,
  person,
  isSelf,
  onClose
}: {
  open: boolean;
  person: User;
  isSelf: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const reset = useResetPassword();
  const [password, setPassword] = useState('');
  // Being an administrator is not the same as having proved you are at the
  // keyboard, so the server asks for the current password on your own account
  // even when it would not on anybody else's.
  const [currentPassword, setCurrentPassword] = useState('');

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      title={t('users.resetPasswordFor', { name: person.name })}
      description={isSelf ? t('users.resetOwnHint') : t('users.resetHint')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" form="reset-password" loading={reset.isPending}>
            {t('users.resetPassword')}
          </Button>
        </>
      }
    >
      <form
        id="reset-password"
        onSubmit={(event) => {
          event.preventDefault();
          reset.mutate(
            { id: person.id, password, currentPassword: isSelf ? currentPassword : undefined },
            {
              onSuccess: () => {
                setPassword('');
                setCurrentPassword('');
                toast.success(t('users.passwordReset'));
                onClose();
              },
              onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
            }
          );
        }}
      >
        {isSelf && (
          <Field label={t('users.currentPassword')} required>
            {({ id }) => (
              <Input
                id={id}
                type="password"
                autoComplete="current-password"
                value={currentPassword}
                onChange={(event) => setCurrentPassword(event.target.value)}
                required
              />
            )}
          </Field>
        )}

        <Field label={t('users.newPassword')} hint={t('users.passwordHint')} required>
          {({ id, describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              type="password"
              autoComplete="new-password"
              minLength={8}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          )}
        </Field>
      </form>
    </Dialog>
  );
}

function NewUserDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const createUser = useCreateUser();

  const empty: NewUser = { username: '', name: '', email: '', password: '', role: 'member' };
  const [form, setForm] = useState<NewUser>(empty);
  const set = (patch: Partial<NewUser>) => setForm((current) => ({ ...current, ...patch }));

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      title={t('users.new')}
      description={t('users.newHint')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" form="new-user" loading={createUser.isPending}>
            {t('common.create')}
          </Button>
        </>
      }
    >
      <form
        id="new-user"
        style={{ display: 'grid', gap: 'var(--space-4)' }}
        onSubmit={(event) => {
          event.preventDefault();
          createUser.mutate(form, {
            onSuccess: (created) => {
              setForm(empty);
              toast.success(t('users.created', { name: created.name }));
              onClose();
            },
            onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
          });
        }}
      >
        <Field label={t('users.name')} required>
          {({ id }) => (
            <Input id={id} value={form.name} onChange={(e) => set({ name: e.target.value })} required />
          )}
        </Field>

        <Field label={t('auth.username')} hint={t('users.usernameHint')} required>
          {({ id, describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              value={form.username}
              onChange={(e) => set({ username: e.target.value })}
              required
            />
          )}
        </Field>

        <Field label={t('users.email')} required>
          {({ id }) => (
            <Input
              id={id}
              type="email"
              value={form.email}
              onChange={(e) => set({ email: e.target.value })}
              required
            />
          )}
        </Field>

        <Field label={t('users.newPassword')} hint={t('users.passwordHint')} required>
          {({ id, describedBy }) => (
            <Input
              id={id}
              aria-describedby={describedBy}
              type="password"
              autoComplete="new-password"
              minLength={8}
              value={form.password}
              onChange={(e) => set({ password: e.target.value })}
              required
            />
          )}
        </Field>

        <Field label={t('users.role')} hint={t('users.roleHint')}>
          {({ id, describedBy }) => (
            <select
              id={id}
              aria-describedby={describedBy}
              className={controls.input}
              value={form.role}
              onChange={(e) => set({ role: e.target.value as NewUser['role'] })}
            >
              {ROLES.map((role) => (
                <option key={role} value={role}>
                  {t(`users.role_${role}`)}
                </option>
              ))}
            </select>
          )}
        </Field>
      </form>
    </Dialog>
  );
}

/** Kept for the empty state's icon, and so the import is obviously deliberate. */
export const UsersIcon = Users;
