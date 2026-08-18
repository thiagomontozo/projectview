import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { PageHeader } from '../app/AppShell';
import { Badge, Card, CardHeader } from '../ui/display';
import { Button } from '../ui/Button';
import { SkeletonList } from '../ui/Skeleton';
import { useAuth } from '../lib/auth';
import { useAdminSettings } from '../lib/enterprise';
import { useAttachmentConfig } from '../lib/queries';
import styles from './apps.module.css';

/**
 * Apps: what this installation is connected to, and what it could be.
 *
 * Not a marketplace. A marketplace implies somebody publishing to it, and an
 * internal tool with three integrations and no third-party developers would be
 * an empty shop with the lights on. What is genuinely useful — and what nobody
 * could see before — is the answer to "is our directory actually wired up?",
 * which until now meant reading an environment file or opening five settings
 * sections and inferring it.
 *
 * So each card is a real integration, with its real state read from the running
 * configuration, and a link to the screen that turns it on.
 */

interface AppEntry {
  key: string;
  /** Which managed setting decides it is on. */
  flag?: string;
  configuredBy: 'settings' | 'environment';
  to: string;
}

const APPS: AppEntry[] = [
  { key: 'ad', flag: 'AD_ENABLED', configuredBy: 'settings', to: '/admin/settings' },
  { key: 'smtp', flag: 'SMTP_ENABLED', configuredBy: 'settings', to: '/admin/settings' },
  { key: 'oidc', flag: 'OIDC_ENABLED', configuredBy: 'settings', to: '/admin/settings' },
  { key: 'ai', flag: 'AI_ENABLED', configuredBy: 'settings', to: '/admin/settings' },
  { key: 'storage', configuredBy: 'environment', to: '/admin/settings' },
  { key: 'webhooks', configuredBy: 'environment', to: '/settings' }
];

export default function AppsPage() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';

  // Only an administrator may read the settings, so for everybody else this
  // shows what exists without claiming to know whether it is on. Guessing would
  // be worse than not saying.
  const settings = useAdminSettings(isAdmin);
  const attachments = useAttachmentConfig();

  function stateOf(app: AppEntry): 'on' | 'off' | 'unknown' {
    if (app.key === 'storage') {
      return attachments.data?.enabled ? 'on' : 'off';
    }
    if (!isAdmin || !settings.data) return 'unknown';
    const setting = settings.data.settings.find((entry) => entry.key === app.flag);
    return setting?.value === 'true' ? 'on' : 'off';
  }

  if (isAdmin && settings.isLoading) {
    return (
      <>
        <PageHeader title={t('apps.title')} description={t('apps.hint')} />
        <SkeletonList rows={3} height={90} label={t('common.loading')} />
      </>
    );
  }

  return (
    <>
      <PageHeader title={t('apps.title')} description={t('apps.hint')} />

      <div className={styles.grid}>
        {APPS.map((app) => {
          const state = stateOf(app);
          return (
            <Card key={app.key}>
              <CardHeader
                title={t(`apps.${app.key}`)}
                action={
                  <Badge tone={state === 'on' ? 'success' : state === 'off' ? 'neutral' : 'warning'}>
                    {t(`apps.state_${state}`)}
                  </Badge>
                }
              />
              <p className={styles.body}>{t(`apps.${app.key}Body`)}</p>
              <div className={styles.footer}>
                <span className={styles.muted}>
                  {t(app.configuredBy === 'settings' ? 'apps.viaSettings' : 'apps.viaEnvironment')}
                </span>
                {isAdmin && (
                  <Button size="sm" variant="ghost" asChild>
                    <Link to={app.to}>{t('apps.configure')}</Link>
                  </Button>
                )}
              </div>
            </Card>
          );
        })}
      </div>

      {/* Said plainly rather than implied by an empty grid: there is no
          third-party app catalogue here, and the honest reason is that an
          internal tool has no third-party developers to fill one. */}
      <Card>
        <CardHeader title={t('apps.moreTitle')} />
        <p className={styles.body}>{t('apps.moreBody')}</p>
      </Card>
    </>
  );
}
