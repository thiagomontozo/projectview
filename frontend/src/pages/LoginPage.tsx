import { useState, type FormEvent } from 'react';
import { Navigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { useAuth } from '../lib/auth';
import { errorMessage } from '../lib/api';
import { useSSOConfig } from '../lib/enterprise';
import { Button } from '../ui/Button';
import { Field, Input } from '../ui/Field';
import controls from '../ui/controls.module.css';
import styles from './LoginPage.module.css';

export default function LoginPage() {
  const { t } = useTranslation();
  const { user, loading, adEnabled, signIn, expiredNotice, clearExpiredNotice } = useAuth();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [mode, setMode] = useState<'ad' | 'local'>('ad');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const sso = useSSOConfig();
  // Single sign-on failures come back as a redirect carrying the reason, since
  // the browser left this page entirely to attempt them.
  const [searchParams] = useSearchParams();
  const ssoError = searchParams.get('error');

  // Redirect declaratively. Navigating during render, as the previous version
  // did, mutates router state mid-render and warns in React 18.
  if (!loading && user) return <Navigate to="/" replace />;

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError('');
    clearExpiredNotice();
    setBusy(true);
    try {
      await signIn(username, password, adEnabled ? mode : 'local');
    } catch (err) {
      setError(errorMessage(err, t('auth.failed')));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.page}>
      <main className={styles.card}>
        <div className={styles.brand}>
          <span className={styles.brandMark} aria-hidden="true">
            PV
          </span>
          <div>
            <h1 className={styles.brandName}>{t('app.name')}</h1>
            <p className={styles.brandTagline}>{t('app.tagline')}</p>
          </div>
        </div>

        <p className={styles.welcome}>{t('auth.welcome')}</p>

        {expiredNotice && (
          <p className={styles.notice} role="status">
            {t('auth.sessionExpired')}
          </p>
        )}

        {adEnabled && (
          <div className={controls.segmented} role="tablist" aria-label={t('auth.signIn')}>
            <button
              type="button"
              role="tab"
              aria-selected={mode === 'ad'}
              className={clsx(controls.segment, mode === 'ad' && controls.segmentActive)}
              onClick={() => setMode('ad')}
            >
              {t('auth.corporateAccount')}
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={mode === 'local'}
              className={clsx(controls.segment, mode === 'local' && controls.segmentActive)}
              onClick={() => setMode('local')}
            >
              {t('auth.localAccount')}
            </button>
          </div>
        )}

        <form className={styles.form} onSubmit={handleSubmit}>
          <Field label={t('auth.username')} required>
            {({ id, invalid }) => (
              <Input
                id={id}
                invalid={invalid}
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder={t('auth.usernamePlaceholder')}
                autoComplete="username"
                autoFocus
                required
              />
            )}
          </Field>

          <Field label={t('auth.password')} required>
            {({ id, invalid }) => (
              <Input
                id={id}
                invalid={invalid}
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
              />
            )}
          </Field>

          {(error || ssoError) && (
            <p className={styles.error} role="alert">
              {error || ssoError}
            </p>
          )}

          <Button type="submit" variant="primary" size="lg" block loading={busy}>
            {busy ? t('auth.signingIn') : t('auth.signIn')}
          </Button>

          {sso.data?.enabled && (
            <>
              <div className={styles.separator}>
                <span>{t('auth.or')}</span>
              </div>
              {/* A link, not a fetch: the whole point of the flow is that the
                  browser leaves for the identity provider and comes back. */}
              <Button
                type="button"
                variant="secondary"
                size="lg"
                block
                onClick={() => window.location.assign('/api/auth/oidc/start')}
              >
                {t('auth.signInWithSSO')}
              </Button>
            </>
          )}
        </form>
      </main>
    </div>
  );
}
