import * as DialogPrimitive from '@radix-ui/react-dialog';
import clsx from 'clsx';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import styles from './overlays.module.css';
import { Button } from './Button';
import { X } from './icons';

interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  size?: 'md' | 'lg';
}

/**
 * Modal dialog.
 *
 * Radix supplies the parts that are easy to get wrong by hand: focus is
 * trapped inside while open and restored to the trigger on close, the rest of
 * the page is marked aria-hidden, Escape closes, and scroll is locked. The
 * title is wired to aria-labelledby automatically.
 */
export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  size = 'md'
}: DialogProps) {
  const { t } = useTranslation();

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className={styles.overlay} />
        <DialogPrimitive.Content className={clsx(styles.dialog, size === 'lg' && styles.dialogLarge)}>
          <div className={styles.dialogHeader}>
            <div>
              <DialogPrimitive.Title className={styles.dialogTitle}>{title}</DialogPrimitive.Title>
              {description && (
                <DialogPrimitive.Description className={styles.dialogDescription}>
                  {description}
                </DialogPrimitive.Description>
              )}
            </div>
            <DialogPrimitive.Close asChild>
              <Button variant="ghost" size="sm" iconOnly aria-label={t('common.close')}>
                <X size={16} />
              </Button>
            </DialogPrimitive.Close>
          </div>

          <div className={styles.dialogBody}>{children}</div>

          {footer && <div className={styles.dialogFooter}>{footer}</div>}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void;
  loading?: boolean;
  destructive?: boolean;
}

/**
 * Confirmation for an action that cannot be undone. Separate from Dialog so
 * that destructive flows all look and behave the same way.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  loading,
  destructive = true
}: ConfirmDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={loading}>
            {t('common.cancel')}
          </Button>
          <Button variant={destructive ? 'danger' : 'primary'} onClick={onConfirm} loading={loading}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <span className="sr-only">{description}</span>
    </Dialog>
  );
}

export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;
