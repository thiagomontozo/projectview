import * as DropdownMenuPrimitive from '@radix-ui/react-dropdown-menu';
import * as TooltipPrimitive from '@radix-ui/react-tooltip';
import clsx from 'clsx';
import type { ReactNode } from 'react';
import styles from './overlays.module.css';

/* --- Dropdown menu ---------------------------------------------------------
   Radix handles roving focus, type-ahead, Escape and click-outside, and
   marks the trigger with aria-expanded — the behaviours that make a menu
   usable from the keyboard.
   ------------------------------------------------------------------------- */

export const Menu = DropdownMenuPrimitive.Root;
export const MenuTrigger = DropdownMenuPrimitive.Trigger;

export function MenuContent({
  children,
  align = 'end',
  sideOffset = 6
}: {
  children: ReactNode;
  align?: 'start' | 'center' | 'end';
  sideOffset?: number;
}) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content className={styles.menu} align={align} sideOffset={sideOffset}>
        {children}
      </DropdownMenuPrimitive.Content>
    </DropdownMenuPrimitive.Portal>
  );
}

export function MenuItem({
  children,
  onSelect,
  danger,
  shortcut,
  disabled
}: {
  children: ReactNode;
  onSelect?: () => void;
  danger?: boolean;
  shortcut?: string;
  disabled?: boolean;
}) {
  return (
    <DropdownMenuPrimitive.Item
      className={clsx(styles.menuItem, danger && styles.menuItemDanger)}
      onSelect={onSelect}
      disabled={disabled}
    >
      {children}
      {shortcut && <span className={styles.menuShortcut}>{shortcut}</span>}
    </DropdownMenuPrimitive.Item>
  );
}

export function MenuLabel({ children }: { children: ReactNode }) {
  return <DropdownMenuPrimitive.Label className={styles.menuLabel}>{children}</DropdownMenuPrimitive.Label>;
}

export function MenuSeparator() {
  return <DropdownMenuPrimitive.Separator className={styles.menuSeparator} />;
}

/* --- Tooltip ----------------------------------------------------------------
   Only ever supplementary. A tooltip is invisible to touch users and to
   anyone navigating by keyboard alone in some browsers, so it must never
   carry the only copy of a control's name — that lives in aria-label.
   -------------------------------------------------------------------------- */

export function TooltipProvider({ children }: { children: ReactNode }) {
  return (
    <TooltipPrimitive.Provider delayDuration={400} skipDelayDuration={300}>
      {children}
    </TooltipPrimitive.Provider>
  );
}

export function Tooltip({
  content,
  children,
  side = 'bottom'
}: {
  content: string;
  children: ReactNode;
  side?: 'top' | 'right' | 'bottom' | 'left';
}) {
  return (
    <TooltipPrimitive.Root>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content className={styles.tooltip} side={side} sideOffset={6}>
          {content}
          <TooltipPrimitive.Arrow className={styles.tooltipArrow} />
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}
