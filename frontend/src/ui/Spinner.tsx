import styles from './display.module.css';

interface SpinnerProps {
  size?: number;
  /** Announced label. Omit inside a control that already conveys the busy
   *  state (a button with aria-busy), so it is not announced twice. */
  label?: string;
}

export function Spinner({ size = 16, label }: SpinnerProps) {
  return (
    <svg
      className={styles.spinner}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      role={label ? 'status' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2.5" opacity="0.2" />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        strokeWidth="2.5"
        strokeLinecap="round"
      />
    </svg>
  );
}
