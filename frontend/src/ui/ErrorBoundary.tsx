import { Component, type ErrorInfo, type ReactNode } from 'react';
import { withTranslation, type WithTranslation } from 'react-i18next';
import { Button } from './Button';
import { Card } from './display';
import { AlertTriangle } from './icons';

interface Props extends WithTranslation {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

/**
 * Catches render errors so one broken component does not blank the whole app.
 *
 * Still a class: error boundaries have no hook equivalent, because React needs
 * a lifecycle method it can call while unwinding a failed render.
 */
class ErrorBoundaryInner extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Kept for the browser console; a real deployment would forward this to
    // an error tracker here.
    console.error('Unhandled render error', error, info.componentStack);
  }

  render() {
    const { error } = this.state;
    const { t, children } = this.props;

    if (!error) return children;

    return (
      <div style={{ padding: 'var(--space-8)', display: 'grid', placeItems: 'center', minHeight: '60vh' }}>
        <Card style={{ maxWidth: 520, textAlign: 'center' }}>
          <div
            style={{
              width: 44,
              height: 44,
              margin: '0 auto var(--space-3)',
              display: 'grid',
              placeItems: 'center',
              borderRadius: 'var(--radius-lg)',
              background: 'var(--danger-subtle)',
              color: 'var(--danger)'
            }}
          >
            <AlertTriangle size={22} />
          </div>
          <h2 style={{ fontSize: 'var(--text-md)', marginBottom: 'var(--space-2)' }}>
            {t('errors.boundaryTitle')}
          </h2>
          <p style={{ color: 'var(--text-secondary)', marginBottom: 'var(--space-4)' }}>
            {t('errors.boundaryBody')}
          </p>
          <pre
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 'var(--text-xs)',
              color: 'var(--text-muted)',
              background: 'var(--surface-sunken)',
              padding: 'var(--space-3)',
              borderRadius: 'var(--radius-sm)',
              textAlign: 'left',
              overflowX: 'auto',
              marginBottom: 'var(--space-4)'
            }}
          >
            {error.message}
          </pre>
          <Button variant="primary" onClick={() => window.location.reload()}>
            {t('errors.reload')}
          </Button>
        </Card>
      </div>
    );
  }
}

export const ErrorBoundary = withTranslation()(ErrorBoundaryInner);
