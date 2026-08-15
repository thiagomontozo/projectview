import * as AvatarPrimitive from '@radix-ui/react-avatar';
import clsx from 'clsx';
import type { HTMLAttributes, ReactNode } from 'react';
import styles from './display.module.css';
import { AlertTriangle } from './icons';
import { Button } from './Button';

/* --- Card ------------------------------------------------------------------ */

// HTMLElement rather than HTMLDivElement: the element is chosen by the caller
// via `as`, and narrowing to a div makes every other tag a type error.
interface CardProps extends HTMLAttributes<HTMLElement> {
  padded?: boolean;
  interactive?: boolean;
  as?: 'div' | 'article' | 'section' | 'li';
}

export function Card({ padded = true, interactive, className, as: Tag = 'div', ...props }: CardProps) {
  return (
    <Tag
      className={clsx(styles.card, padded && styles.cardPadded, interactive && styles.cardInteractive, className)}
      {...props}
    />
  );
}

export function CardHeader({ title, action }: { title: ReactNode; action?: ReactNode }) {
  return (
    <div className={styles.cardHeader}>
      <h3 className={styles.cardTitle}>{title}</h3>
      {action}
    </div>
  );
}

/* --- Badge ------------------------------------------------------------------ */

export type BadgeTone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger';

const badgeTone: Record<BadgeTone, string> = {
  neutral: styles.badgeNeutral,
  accent: styles.badgeAccent,
  success: styles.badgeSuccess,
  warning: styles.badgeWarning,
  danger: styles.badgeDanger
};

export function Badge({
  tone = 'neutral',
  dot,
  children,
  className
}: {
  tone?: BadgeTone;
  dot?: boolean;
  children: ReactNode;
  className?: string;
}) {
  return (
    <span className={clsx(styles.badge, badgeTone[tone], className)}>
      {dot && <span className={styles.badgeDot} aria-hidden="true" />}
      {children}
    </span>
  );
}

/* --- Avatar ------------------------------------------------------------------ */

interface AvatarProps {
  name: string;
  color?: string;
  size?: number;
  src?: string;
}

/** Derives up to two initials, skipping particles like "de" and "da". */
function initials(name: string): string {
  const parts = name
    .trim()
    .split(/\s+/)
    .filter((p) => p.length > 2 || /^[A-ZÀ-Ý]/.test(p));
  if (parts.length === 0) return '?';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

export function Avatar({ name, color = 'var(--accent)', size = 28, src }: AvatarProps) {
  return (
    <AvatarPrimitive.Root
      className={styles.avatar}
      style={{ width: size, height: size, background: color, fontSize: Math.max(10, size * 0.38) }}
      // The name is on the wrapper, so assistive tech reads the person rather
      // than the two letters that happen to represent them.
      aria-label={name}
      role="img"
    >
      {src && <AvatarPrimitive.Image className={styles.avatarImage} src={src} alt="" />}
      {/* delayMs is left undefined when there is no image to wait for: any
          value, zero included, schedules a timer, which would leave the
          avatar blank for a frame instead of showing the initials at once. */}
      <AvatarPrimitive.Fallback className={styles.avatarFallback} delayMs={src ? 300 : undefined}>
        <span aria-hidden="true">{initials(name)}</span>
      </AvatarPrimitive.Fallback>
    </AvatarPrimitive.Root>
  );
}

export function AvatarGroup({ children }: { children: ReactNode }) {
  return <div className={styles.avatarGroup}>{children}</div>;
}

/* --- Empty state -------------------------------------------------------------- */

interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  body?: string;
  action?: ReactNode;
}

/**
 * Shown when a query succeeded and returned nothing. Distinct from an error:
 * "there is nothing here yet" and "we could not find out" call for different
 * words and different actions.
 */
export function EmptyState({ icon, title, body, action }: EmptyStateProps) {
  return (
    <div className={styles.empty}>
      {icon && <div className={styles.emptyIcon}>{icon}</div>}
      <p className={styles.emptyTitle}>{title}</p>
      {body && <p className={styles.emptyBody}>{body}</p>}
      {action && <div className={styles.emptyActions}>{action}</div>}
    </div>
  );
}

/* --- Error state --------------------------------------------------------------- */

interface ErrorStateProps {
  title: string;
  body?: string;
  detail?: string;
  onRetry?: () => void;
  retryLabel?: string;
}

export function ErrorState({ title, body, detail, onRetry, retryLabel }: ErrorStateProps) {
  return (
    <div className={styles.empty} role="alert">
      <div className={styles.emptyIcon} style={{ color: 'var(--danger)', background: 'var(--danger-subtle)' }}>
        <AlertTriangle size={22} />
      </div>
      <p className={styles.emptyTitle}>{title}</p>
      {body && <p className={styles.emptyBody}>{body}</p>}
      {detail && <pre className={styles.errorDetail}>{detail}</pre>}
      {onRetry && retryLabel && (
        <div className={styles.emptyActions}>
          <Button variant="secondary" onClick={onRetry}>
            {retryLabel}
          </Button>
        </div>
      )}
    </div>
  );
}
