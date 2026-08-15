import * as ToastPrimitive from '@radix-ui/react-toast';
import clsx from 'clsx';
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import styles from './overlays.module.css';
import { Button } from './Button';
import { AlertTriangle, CheckCircle, Info, X } from './icons';

export type ToastTone = 'success' | 'error' | 'info';

interface ToastMessage {
  id: number;
  tone: ToastTone;
  title: string;
  description?: string;
}

interface ToastContextValue {
  toast: (message: Omit<ToastMessage, 'id'>) => void;
  success: (title: string, description?: string) => void;
  error: (title: string, description?: string) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

/**
 * Application-wide notifications.
 *
 * Radix renders each toast into a live region, so a message that appears
 * after an action is announced rather than only seen — which is the whole
 * point of confirming an action that produced no visible change.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [messages, setMessages] = useState<ToastMessage[]>([]);
  const { t } = useTranslation();

  const toast = useCallback((message: Omit<ToastMessage, 'id'>) => {
    setMessages((current) => [...current, { ...message, id: Date.now() + Math.random() }]);
  }, []);

  const value = useMemo<ToastContextValue>(
    () => ({
      toast,
      success: (title, description) => toast({ tone: 'success', title, description }),
      error: (title, description) => toast({ tone: 'error', title, description })
    }),
    [toast]
  );

  const dismiss = (id: number) => setMessages((current) => current.filter((m) => m.id !== id));

  const icons: Record<ToastTone, ReactNode> = {
    success: <CheckCircle size={18} />,
    error: <AlertTriangle size={18} />,
    info: <Info size={18} />
  };

  const toneClass: Record<ToastTone, string> = {
    success: styles.toastSuccess,
    error: styles.toastError,
    info: styles.toastInfo
  };

  return (
    <ToastContext.Provider value={value}>
      <ToastPrimitive.Provider swipeDirection="right" duration={6000}>
        {children}

        {messages.map((message) => (
          <ToastPrimitive.Root
            key={message.id}
            className={clsx(styles.toast, toneClass[message.tone])}
            onOpenChange={(open) => !open && dismiss(message.id)}
            // Errors interrupt; confirmations wait their turn.
            type={message.tone === 'error' ? 'foreground' : 'background'}
          >
            <span className={styles.toastIcon} aria-hidden="true">
              {icons[message.tone]}
            </span>
            <div>
              <ToastPrimitive.Title className={styles.toastTitle}>{message.title}</ToastPrimitive.Title>
              {message.description && (
                <ToastPrimitive.Description className={styles.toastDescription}>
                  {message.description}
                </ToastPrimitive.Description>
              )}
            </div>
            <ToastPrimitive.Close asChild>
              <Button variant="ghost" size="sm" iconOnly aria-label={t('common.dismiss')}>
                <X size={14} />
              </Button>
            </ToastPrimitive.Close>
          </ToastPrimitive.Root>
        ))}

        <ToastPrimitive.Viewport className={styles.toastViewport} />
      </ToastPrimitive.Provider>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used inside a ToastProvider');
  }
  return context;
}
