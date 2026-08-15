import { forwardRef, useId, type InputHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from 'react';
import * as LabelPrimitive from '@radix-ui/react-label';
import clsx from 'clsx';
import styles from './controls.module.css';

interface FieldProps {
  label: string;
  /** Explanatory text shown under the control. */
  hint?: string;
  /** Validation message. Its presence marks the control invalid. */
  error?: string;
  required?: boolean;
  children: (ids: { id: string; describedBy?: string; invalid: boolean }) => ReactNode;
}

/**
 * Wires a label, hint and error message to a control with the aria-* plumbing
 * that makes them announced rather than merely visible.
 *
 * The child is a render prop because the generated ids have to reach the
 * control itself; passing them down explicitly is what guarantees the label
 * and the error are actually associated, rather than just adjacent.
 */
export function Field({ label, hint, error, required, children }: FieldProps) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(' ') || undefined;

  return (
    <div className={styles.field}>
      <LabelPrimitive.Root className={styles.label} htmlFor={id}>
        {label}
        {required && (
          <span className={styles.required} aria-hidden="true">
            *
          </span>
        )}
      </LabelPrimitive.Root>

      {children({ id, describedBy, invalid: Boolean(error) })}

      {hint && !error && (
        <span className={styles.hint} id={hintId}>
          {hint}
        </span>
      )}
      {error && (
        // role="alert" makes the message announced the moment it appears,
        // which is the difference between a sighted-only and a usable form.
        <span className={styles.error} id={errorId} role="alert">
          {error}
        </span>
      )}
    </div>
  );
}

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  invalid?: boolean;
  icon?: ReactNode;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { invalid, icon, className, ...props },
  ref
) {
  const input = (
    <input
      ref={ref}
      className={clsx(styles.input, invalid && styles.inputInvalid, icon && styles.inputWithIcon, className)}
      aria-invalid={invalid || undefined}
      {...props}
    />
  );

  if (!icon) return input;

  return (
    <span className={styles.inputWrap}>
      <span className={styles.inputIcon} aria-hidden="true">
        {icon}
      </span>
      {input}
    </span>
  );
});

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  invalid?: boolean;
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { invalid, className, ...props },
  ref
) {
  return (
    <textarea
      ref={ref}
      className={clsx(styles.textarea, invalid && styles.inputInvalid, className)}
      aria-invalid={invalid || undefined}
      {...props}
    />
  );
});
