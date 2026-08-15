import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { Slot } from '@radix-ui/react-slot';
import clsx from 'clsx';
import styles from './controls.module.css';
import { Spinner } from './Spinner';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'dangerGhost';
export type ButtonSize = 'sm' | 'md' | 'lg';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Renders a spinner and blocks interaction. */
  loading?: boolean;
  block?: boolean;
  /** Renders as the single child element instead of a <button> — used to make
   *  a router Link look and behave like a button without nesting interactive
   *  elements, which is invalid HTML and confuses screen readers. */
  asChild?: boolean;
  /** Required when the button shows only an icon: without it the control is
   *  announced as "button" with no indication of what it does. */
  'aria-label'?: string;
  iconOnly?: boolean;
  children?: ReactNode;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    variant = 'secondary',
    size = 'md',
    loading = false,
    block = false,
    asChild = false,
    iconOnly = false,
    className,
    disabled,
    children,
    ...props
  },
  ref
) {
  const Component = asChild ? Slot : 'button';

  return (
    <Component
      ref={ref}
      className={clsx(
        styles.button,
        styles[variant],
        styles[size],
        iconOnly && styles.iconOnly,
        block && styles.block,
        className
      )}
      disabled={disabled || loading}
      // Announced to assistive tech while a request is in flight, so the
      // state change is not purely visual.
      aria-busy={loading || undefined}
      {...props}
    >
      {loading ? (
        <>
          <Spinner size={size === 'lg' ? 18 : 14} />
          {children}
        </>
      ) : (
        children
      )}
    </Component>
  );
});
