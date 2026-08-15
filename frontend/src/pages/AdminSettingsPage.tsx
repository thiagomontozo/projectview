import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Badge, Card, CardHeader, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Shield } from '../ui/icons';
import controls from '../ui/controls.module.css';
import { useAuth } from '../lib/auth';
import { downloadUrl } from '../lib/api';
import {
  useAdminSettings,
  useSaveAdminSettings,
  useTestAD,
  useTestSMTP,
  type AdminSetting
} from '../lib/enterprise';
import styles from './pages.module.css';

const GROUPS = ['ad', 'smtp', 'oidc', 'alerts', 'retention'] as const;

/**
 * Integration settings, editable without a redeploy.
 *
 * What is *not* here matters as much as what is: the database connection, the
 * token signing secret and the bootstrap admin are absent by design. None of
 * them can be applied without a restart, and an installation able to rewrite
 * its own database URL from a web form is one compromised administrator away
 * from being somebody else's. The server enforces that with an allow-list —
 * this screen only renders what the server admits to managing.
 */
export default function AdminSettingsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';

  const { data, isLoading, isError, refetch } = useAdminSettings(isAdmin);
  const save = useSaveAdminSettings();

  // Only what the operator actually touched is sent. Sending the whole form
  // would store an override for every key, which turns "the deployment
  // configures this" into "somebody set this here" across the board.
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [cleared, setCleared] = useState<string[]>([]);

  const byGroup = useMemo(() => {
    const groups: Record<string, AdminSetting[]> = {};
    for (const setting of data?.settings ?? []) {
      (groups[setting.group] ??= []).push(setting);
    }
    return groups;
  }, [data]);

  if (!isAdmin) {
    return (
      <>
        <PageHeader title={t('admin.title')} />
        <Card>
          <ErrorState title={t('admin.restricted')} body={t('admin.restrictedBody')} />
        </Card>
      </>
    );
  }

  const dirty = Object.keys(edits).length > 0 || cleared.length > 0;

  function submit() {
    save.mutate(
      { values: edits, clear: cleared },
      {
        onSuccess: (result) => {
          setEdits({});
          setCleared([]);
          if (result.warning) toast.error(result.warning);
          else toast.success(t('admin.saved'));
        },
        onError: () => toast.error(t('errors.genericBody'))
      }
    );
  }

  return (
    <>
      <PageHeader
        title={t('admin.title')}
        description={t('admin.hint')}
        actions={
          <>
            <Button variant="secondary" onClick={() => downloadUrl('/settings/env')}>
              {t('admin.downloadEnv')}
            </Button>
            <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={submit}>
              {t('common.save')}
            </Button>
          </>
        }
      />

      {isLoading && <SkeletonList rows={4} height={56} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState
            title={t('errors.loadFailed')}
            onRetry={() => void refetch()}
            retryLabel={t('common.retry')}
          />
        </Card>
      )}

      {data && (
        <>
          <Card>
            <p className={styles.muted}>
              {data.mirror.enabled
                ? t('admin.mirrorOn', { path: data.mirror.path })
                : t('admin.mirrorOff')}
            </p>
          </Card>

          {GROUPS.filter((group) => byGroup[group]?.length).map((group) => (
            <div key={group} className={styles.settingsSection}>
              <Card>
                <CardHeader title={t(`admin.group_${group}`)} />
                <p className={styles.muted}>{t(`admin.groupHint_${group}`)}</p>

                {byGroup[group].map((setting) => (
                  <SettingRow
                    key={setting.key}
                    setting={setting}
                    draft={edits[setting.key]}
                    cleared={cleared.includes(setting.key)}
                    onChange={(value) => setEdits((current) => ({ ...current, [setting.key]: value }))}
                    onReset={() => {
                      setEdits((current) => {
                        const next = { ...current };
                        delete next[setting.key];
                        return next;
                      });
                      setCleared((current) =>
                        current.includes(setting.key)
                          ? current.filter((k) => k !== setting.key)
                          : [...current, setting.key]
                      );
                    }}
                  />
                ))}

                {group === 'smtp' && <TestSMTPRow />}
                {group === 'ad' && <TestADRow />}
              </Card>
            </div>
          ))}
        </>
      )}
    </>
  );
}

function SettingRow({
  setting,
  draft,
  cleared,
  onChange,
  onReset
}: {
  setting: AdminSetting;
  draft?: string;
  cleared: boolean;
  onChange: (value: string) => void;
  onReset: () => void;
}) {
  const { t } = useTranslation();
  const value = draft ?? setting.value ?? '';

  return (
    <div className={styles.settingRow}>
      <label htmlFor={`setting-${setting.key}`} className={styles.settingLabel}>
        <code className={styles.settingKey}>{setting.key}</code>
        <span className={styles.subtle}>
          {setting.overridden ? t('admin.overridden') : t('admin.fromEnvironment')}
        </span>
      </label>

      <div className={styles.settingControl}>
        {setting.kind === 'bool' ? (
          <select
            id={`setting-${setting.key}`}
            className={controls.input}
            value={value || 'false'}
            onChange={(event) => onChange(event.target.value)}
          >
            <option value="true">{t('admin.on')}</option>
            <option value="false">{t('admin.off')}</option>
          </select>
        ) : setting.secret ? (
          <input
            id={`setting-${setting.key}`}
            className={controls.input}
            type="password"
            autoComplete="new-password"
            // A stored secret is never read back, so the field starts empty
            // and an empty field means "leave it alone". Otherwise saving the
            // form without retyping a password would wipe it — the commonest
            // way a settings screen breaks a live system.
            placeholder={setting.isSet ? t('admin.secretSet') : t('admin.secretEmpty')}
            value={draft ?? ''}
            onChange={(event) => onChange(event.target.value)}
          />
        ) : (
          <input
            id={`setting-${setting.key}`}
            className={controls.input}
            type={setting.kind === 'number' ? 'number' : 'text'}
            value={value}
            onChange={(event) => onChange(event.target.value)}
          />
        )}

        {setting.overridden && (
          <Button size="sm" variant={cleared ? 'primary' : 'ghost'} onClick={onReset}>
            {cleared ? t('admin.willReset') : t('admin.reset')}
          </Button>
        )}
      </div>
    </div>
  );
}

/** Sends a real message with the settings in force, not with the form's draft. */
function TestSMTPRow() {
  const { t } = useTranslation();
  const toast = useToast();
  const test = useTestSMTP();
  const [to, setTo] = useState('');

  return (
    <div className={styles.settingRow}>
      <label htmlFor="smtp-test-to">
        {t('admin.testSmtp')}
        <span className={styles.subtle} style={{ display: 'block' }}>
          {t('admin.testHint')}
        </span>
      </label>
      <div className={styles.settingControl}>
        <input
          id="smtp-test-to"
          className={controls.input}
          type="email"
          placeholder={t('admin.testTo')}
          value={to}
          onChange={(event) => setTo(event.target.value)}
        />
        <Button
          size="sm"
          variant="secondary"
          loading={test.isPending}
          onClick={() =>
            test.mutate(
              { to },
              {
                onSuccess: (result) =>
                  result.ok
                    ? toast.success(t('admin.testSent', { to: result.sentTo }))
                    : toast.error(result.error ?? t('errors.genericBody')),
                onError: (error) => toast.error(String(error))
              }
            )
          }
        >
          {t('admin.runTest')}
        </Button>
      </div>
    </div>
  );
}

/** Binds against the directory with credentials that are never stored. */
function TestADRow() {
  const { t } = useTranslation();
  const toast = useToast();
  const test = useTestAD();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');

  return (
    <div className={styles.settingRow}>
      <label htmlFor="ad-test-user">
        {t('admin.testAd')}
        <span className={styles.subtle} style={{ display: 'block' }}>
          {t('admin.testAdHint')}
        </span>
      </label>
      <div className={styles.settingControl}>
        <input
          id="ad-test-user"
          className={controls.input}
          placeholder={t('auth.username')}
          value={username}
          onChange={(event) => setUsername(event.target.value)}
        />
        <input
          className={controls.input}
          type="password"
          autoComplete="off"
          placeholder={t('auth.password')}
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
        <Button
          size="sm"
          variant="secondary"
          loading={test.isPending}
          disabled={!username || !password}
          onClick={() =>
            test.mutate(
              { username, password },
              {
                onSuccess: (result) => {
                  setPassword('');
                  return result.ok
                    ? toast.success(t('admin.testBound', { name: result.name }))
                    : toast.error(result.error ?? t('errors.genericBody'));
                },
                onError: (error) => toast.error(String(error))
              }
            )
          }
        >
          {t('admin.runTest')}
        </Button>
      </div>
    </div>
  );
}

/** Shown in the sidebar so administrators can find the screen at all. */
export function AdminBadge() {
  const { t } = useTranslation();
  return (
    <Badge tone="accent">
      <Shield size={12} /> {t('admin.title')}
    </Badge>
  );
}
