import { useTranslation } from 'react-i18next';
import * as SwitchPrimitive from '@radix-ui/react-switch';
import { PageHeader } from '../app/AppShell';
import { Card, CardHeader, EmptyState, ErrorState, Avatar, Badge } from '../ui/display';
import { Button } from '../ui/Button';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { useAuth } from '../lib/auth';
import { useTheme, type ThemePreference } from '../lib/theme';
import { useRevokeSession, useSessions } from '../lib/queries';
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

      <SessionsCard />
    </>
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
