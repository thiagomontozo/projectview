import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import * as SwitchPrimitive from '@radix-ui/react-switch';
import { PageHeader } from '../app/AppShell';
import { Card, CardHeader, EmptyState, ErrorState, Avatar, Badge } from '../ui/display';
import { Button } from '../ui/Button';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { useAuth } from '../lib/auth';
import { useTheme, type ThemePreference } from '../lib/theme';
import {
  useNotificationPreferences,
  useRevokeSession,
  useSaveNotificationPreferences,
  useSessions
} from '../lib/queries';
import {
  useCreateServiceToken,
  useRevokeServiceToken,
  useServiceTokens,
  useUpdateUser
} from '../lib/enterprise';
import { downloadUrl } from '../lib/api';
import { SUPPORTED_LANGUAGES } from '../i18n';
import { Shield, Monitor } from '../ui/icons';
import controls from '../ui/controls.module.css';
import styles from './pages.module.css';
import clsx from 'clsx';

export default function SettingsPage() {
  const { t, i18n } = useTranslation();
  const { user } = useAuth();
  const { preference, setPreference } = useTheme();

  return (
    <>
      <PageHeader title={t('settings.title')} />

      <div className={styles.settingsSection}>
        <Card>
          <CardHeader title={t('settings.appearance')} />
          <p className={styles.muted} style={{ marginBottom: 'var(--space-4)' }}>
            {t('settings.appearanceHint')}
          </p>

          <div className={styles.settingRow}>
            <label htmlFor="theme-select">{t('theme.label')}</label>
            <div className={controls.segmented}>
              {(['light', 'dark', 'system'] as ThemePreference[]).map((option) => (
                <button
                  key={option}
                  type="button"
                  className={clsx(controls.segment, preference === option && controls.segmentActive)}
                  aria-pressed={preference === option}
                  onClick={() => setPreference(option)}
                >
                  {t(`theme.${option}`)}
                </button>
              ))}
            </div>
          </div>

          <div className={styles.settingRow}>
            <label htmlFor="language-select">{t('language.label')}</label>
            <div className={controls.segmented}>
              {SUPPORTED_LANGUAGES.map(({ code, label }) => (
                <button
                  key={code}
                  type="button"
                  className={clsx(controls.segment, i18n.language === code && controls.segmentActive)}
                  aria-pressed={i18n.language === code}
                  onClick={() => void i18n.changeLanguage(code)}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>
        </Card>
      </div>

      <div className={styles.settingsSection}>
        <Card>
          <CardHeader title={t('settings.profile')} />
          <div className={styles.personCell}>
            <Avatar name={user?.name ?? '?'} color={user?.avatarColor} size={40} />
            <div>
              <div style={{ fontWeight: 'var(--weight-medium)' }}>{user?.name}</div>
              <div className={styles.subtle}>{user?.email}</div>
            </div>
            {user?.role && <Badge tone="accent">{user.role}</Badge>}
          </div>
        </Card>
      </div>

      <NotificationPreferencesCard />

      <CapacityCard />

      <PrivacyCard />

      <SessionsCard />

      {user?.role === 'admin' && <ServiceTokensCard />}
    </>
  );
}

const NOTIFICATION_TYPES = [
  'task_assigned',
  'task_due_soon',
  'task_overdue',
  'comment_mention',
  'chat_message'
] as const;

/**
 * Per-type delivery choices, a digest, and quiet hours.
 *
 * The point of the digest is that someone can turn immediate e-mail off
 * without going blind: notifications still accumulate in the app and arrive
 * once a day as a single message. Quiet hours do the same for a window.
 */
function NotificationPreferencesCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const { data: prefs, isLoading } = useNotificationPreferences();
  const save = useSaveNotificationPreferences();

  if (isLoading || !prefs) {
    return (
      <Card>
        <CardHeader title={t('settings.notifications')} />
        <SkeletonList rows={3} height={36} label={t('common.loading')} />
      </Card>
    );
  }

  const update = (next: Partial<typeof prefs>) =>
    save.mutate({ ...prefs, ...next }, { onSuccess: () => toast.success(t('settings.preferencesSaved')) });

  const toggleChannel = (type: string, channel: 'inApp' | 'email') => {
    const current = prefs.channels[type] ?? { inApp: true, email: false };
    update({
      channels: { ...prefs.channels, [type]: { ...current, [channel]: !current[channel] } }
    });
  };

  return (
    <div className={styles.settingsSection}>
      <Card>
        <CardHeader title={t('settings.notifications')} />
        <p className={styles.muted} style={{ marginBottom: 'var(--space-4)' }}>
          {t('settings.notificationsHint')}
        </p>

        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">{t('settings.notificationType')}</th>
                <th scope="col">{t('settings.inApp')}</th>
                <th scope="col">{t('settings.email')}</th>
              </tr>
            </thead>
            <tbody>
              {NOTIFICATION_TYPES.map((type) => {
                const channel = prefs.channels[type] ?? { inApp: true, email: false };
                return (
                  <tr key={type}>
                    <td>{t(`settings.notif_${type}`)}</td>
                    <td>
                      <SwitchPrimitive.Root
                        className={controls.switchRoot}
                        checked={channel.inApp}
                        onCheckedChange={() => toggleChannel(type, 'inApp')}
                        aria-label={`${t(`settings.notif_${type}`)} — ${t('settings.inApp')}`}
                      >
                        <SwitchPrimitive.Thumb className={controls.switchThumb} />
                      </SwitchPrimitive.Root>
                    </td>
                    <td>
                      <SwitchPrimitive.Root
                        className={controls.switchRoot}
                        checked={channel.email}
                        onCheckedChange={() => toggleChannel(type, 'email')}
                        aria-label={`${t(`settings.notif_${type}`)} — ${t('settings.email')}`}
                      >
                        <SwitchPrimitive.Thumb className={controls.switchThumb} />
                      </SwitchPrimitive.Root>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        <div className={styles.settingRow} style={{ marginTop: 'var(--space-4)' }}>
          <label>
            {t('settings.digest')}
            <span className={styles.subtle} style={{ display: 'block' }}>
              {t('settings.digestHint')}
            </span>
          </label>
          <div className={controls.segmented}>
            {(['off', 'daily', 'weekly'] as const).map((option) => (
              <button
                key={option}
                type="button"
                className={clsx(controls.segment, prefs.digest === option && controls.segmentActive)}
                aria-pressed={prefs.digest === option}
                onClick={() => update({ digest: option })}
              >
                {t(`settings.digest_${option}`)}
              </button>
            ))}
          </div>
        </div>

        {prefs.digest !== 'off' && (
          <div className={styles.settingRow}>
            <label htmlFor="digest-hour">{t('settings.digestHour')}</label>
            <select
              id="digest-hour"
              className={controls.input}
              style={{ width: 100 }}
              value={prefs.digestHour}
              onChange={(event) => update({ digestHour: Number(event.target.value) })}
            >
              {Array.from({ length: 24 }, (_, hour) => (
                <option key={hour} value={hour}>
                  {String(hour).padStart(2, '0')}:00
                </option>
              ))}
            </select>
          </div>
        )}
      </Card>
    </div>
  );
}

/**
 * The hours a week this person is available for.
 *
 * Capacity planning compares committed work against this number; without it
 * the report would have to assume everyone is a full-time forty, which is the
 * assumption that makes such reports useless in an organisation with part-time
 * people and shared allocations.
 */
function CapacityCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const { user } = useAuth();
  const updateUser = useUpdateUser();
  const [hours, setHours] = useState<string>();

  const current = hours ?? String(user?.weeklyCapacityHours ?? 40);

  return (
    <div className={styles.settingsSection}>
      <Card>
        <CardHeader title={t('settings.capacity')} />
        <p className={styles.muted}>{t('settings.capacityHint')}</p>

        <div className={styles.settingRow}>
          <label htmlFor="weekly-capacity">{t('settings.weeklyHours')}</label>
          <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
            <input
              id="weekly-capacity"
              className={controls.input}
              style={{ width: 100 }}
              type="number"
              min={0}
              max={168}
              value={current}
              onChange={(event) => setHours(event.target.value)}
            />
            <Button
              size="sm"
              variant="secondary"
              loading={updateUser.isPending}
              onClick={() =>
                user &&
                updateUser.mutate(
                  { id: user.id, weeklyCapacityHours: Number(current) },
                  { onSuccess: () => toast.success(t('settings.preferencesSaved')) }
                )
              }
            >
              {t('common.save')}
            </Button>
          </div>
        </div>
      </Card>
    </div>
  );
}

/**
 * The export half of the two privacy rights.
 *
 * Erasure is deliberately not here: it is irreversible, it is an
 * administrator's action, and a button next to "download my data" is a button
 * somebody eventually presses by accident.
 */
function PrivacyCard() {
  const { t } = useTranslation();
  const { user } = useAuth();

  return (
    <div className={styles.settingsSection}>
      <Card>
        <CardHeader title={t('settings.privacy')} />
        <p className={styles.muted}>{t('settings.privacyHint')}</p>
        <Button variant="secondary" disabled={!user} onClick={() => user && downloadUrl(`/users/${user.id}/data-export`)}>
          {t('settings.downloadMyData')}
        </Button>
      </Card>
    </div>
  );
}

/**
 * Machine credentials, for SCIM provisioning and read-only reporting.
 *
 * The secret is shown exactly once, in the response that created it. Only its
 * hash is stored, so there is no second chance and the screen says so.
 */
function ServiceTokensCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const { data: tokens, isLoading } = useServiceTokens();
  const createToken = useCreateServiceToken();
  const revokeToken = useRevokeServiceToken();

  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<string[]>(['scim']);
  const [issued, setIssued] = useState<string>();

  const toggleScope = (scope: string) =>
    setScopes((current) =>
      current.includes(scope) ? current.filter((s) => s !== scope) : [...current, scope]
    );

  return (
    <div className={styles.settingsSection}>
      <Card>
        <CardHeader title={t('settings.serviceTokens')} />
        <p className={styles.muted}>{t('settings.serviceTokensHint')}</p>

        {issued && (
          <div className={styles.tokenReveal} role="alert">
            <strong>{t('settings.tokenShownOnce')}</strong>
            <code className={styles.tokenSecret}>{issued}</code>
          </div>
        )}

        <div className={styles.settingRow}>
          <input
            className={controls.input}
            placeholder={t('settings.tokenName')}
            aria-label={t('settings.tokenName')}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
          <div style={{ display: 'flex', gap: 'var(--space-3)', alignItems: 'center' }}>
            {['scim', 'reports'].map((scope) => (
              <label key={scope} className={styles.subtle} style={{ display: 'flex', gap: 4 }}>
                <input type="checkbox" checked={scopes.includes(scope)} onChange={() => toggleScope(scope)} />
                {scope}
              </label>
            ))}
            <Button
              size="sm"
              variant="primary"
              disabled={!name || scopes.length === 0}
              loading={createToken.isPending}
              onClick={() =>
                createToken.mutate(
                  { name, scopes },
                  {
                    onSuccess: (token) => {
                      setIssued(token.secret);
                      setName('');
                      toast.success(t('settings.tokenCreated'));
                    },
                    onError: () => toast.error(t('errors.genericBody'))
                  }
                )
              }
            >
              {t('common.create')}
            </Button>
          </div>
        </div>

        {isLoading && <SkeletonList rows={2} height={36} label={t('common.loading')} />}

        {tokens?.map((token) => (
          <div key={token.id} className={styles.sessionRow}>
            <Shield size={18} />
            <div className={styles.sessionInfo}>
              <div className={styles.sessionAgent}>{token.name}</div>
              <div className={styles.subtle}>{token.scopes.join(', ') || '—'}</div>
            </div>
            {token.revokedAt ? (
              <Badge tone="neutral">{t('settings.revoked')}</Badge>
            ) : (
              <Button
                variant="dangerGhost"
                size="sm"
                loading={revokeToken.isPending && revokeToken.variables === token.id}
                onClick={() =>
                  revokeToken.mutate(token.id, { onSuccess: () => toast.success(t('settings.revoked')) })
                }
              >
                {t('settings.revoke')}
              </Button>
            )}
          </div>
        ))}
      </Card>
    </div>
  );
}

/**
 * Surfaces the server-side sessions introduced with revocable logins: without
 * a screen, "you can end a session" is a capability nobody can reach.
 */
function SessionsCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const { data: sessions, isLoading, isError, refetch } = useSessions();
  const revoke = useRevokeSession();

  const formatDate = (value?: string) =>
    value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value)) : '—';

  return (
    <Card>
      <CardHeader title={t('settings.sessions')} />
      <p className={styles.muted} style={{ marginBottom: 'var(--space-4)' }}>
        {t('settings.sessionsHint')}
      </p>

      {isLoading && <SkeletonList rows={2} height={44} label={t('common.loading')} />}

      {isError && (
        <ErrorState
          title={t('errors.loadFailed')}
          onRetry={() => void refetch()}
          retryLabel={t('common.retry')}
        />
      )}

      {sessions?.length === 0 && <EmptyState icon={<Shield size={20} />} title={t('settings.sessions')} />}

      {sessions?.map((session) => (
        <div key={session.id} className={styles.sessionRow}>
          <Monitor size={18} />
          <div className={styles.sessionInfo}>
            <div className={styles.sessionAgent}>{session.userAgent || session.ip || session.id}</div>
            <div className={styles.subtle}>
              {t('settings.lastUsed')}: {formatDate(session.lastUsedAt ?? session.createdAt)}
            </div>
          </div>

          {session.current ? (
            <Badge tone="success">{t('settings.currentSession')}</Badge>
          ) : (
            <Button
              variant="dangerGhost"
              size="sm"
              loading={revoke.isPending && revoke.variables === session.id}
              onClick={() =>
                revoke.mutate(session.id, {
                  onSuccess: () => toast.success(t('settings.revoked'))
                })
              }
            >
              {t('settings.revoke')}
            </Button>
          )}
        </div>
      ))}
    </Card>
  );
}
