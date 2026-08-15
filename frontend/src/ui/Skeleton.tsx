import clsx from 'clsx';
import styles from './display.module.css';

interface SkeletonProps {
  width?: number | string;
  height?: number | string;
  radius?: string;
  className?: string;
}

/**
 * A placeholder shaped like the content it stands in for.
 *
 * Marked aria-hidden and paired with a live region by the caller: announcing
 * every shimmering block would flood a screen reader with noise, so the
 * loading state is communicated once, in words, instead.
 */
export function Skeleton({ width = '100%', height = 16, radius, className }: SkeletonProps) {
  return (
    <div
      className={clsx(styles.skeleton, className)}
      style={{ width, height, borderRadius: radius }}
      aria-hidden="true"
    />
  );
}

interface SkeletonListProps {
  rows?: number;
  height?: number;
  gap?: string;
  /** Announced while the placeholder is on screen. */
  label: string;
}

export function SkeletonList({ rows = 5, height = 56, gap = 'var(--space-3)', label }: SkeletonListProps) {
  return (
    <div role="status" aria-live="polite" aria-busy="true">
      <span className="sr-only">{label}</span>
      <div style={{ display: 'flex', flexDirection: 'column', gap }}>
        {Array.from({ length: rows }, (_, i) => (
          <Skeleton key={i} height={height} radius="var(--radius-lg)" />
        ))}
      </div>
    </div>
  );
}
