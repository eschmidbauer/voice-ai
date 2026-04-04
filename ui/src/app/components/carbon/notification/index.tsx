import type { FC, ReactNode } from 'react';
import { InlineNotification, ActionableNotification } from '@carbon/react';
import { cn } from '@/utils';

// ─── Types ───────────────────────────────────────────────────────────────────

type NotificationKind = 'info' | 'success' | 'warning' | 'error';

// ─── Inline Notification ─────────────────────────────────────────────────────

export interface CarbonNotificationProps {
  kind: NotificationKind;
  title: string;
  subtitle?: string;
  className?: string;
  lowContrast?: boolean;
  hideCloseButton?: boolean;
  onClose?: () => void;
}

/** Carbon InlineNotification — static notification banner. */
export const Notification: FC<CarbonNotificationProps> = ({
  kind,
  title,
  subtitle,
  className,
  lowContrast = true,
  hideCloseButton = true,
  onClose,
}) => (
  <InlineNotification
    kind={kind}
    title={title}
    subtitle={subtitle}
    lowContrast={lowContrast}
    hideCloseButton={hideCloseButton}
    onCloseButtonClick={onClose}
    className={cn('!max-w-full', className)}
  />
);

// ─── Actionable Notification ─────────────────────────────────────────────────

export interface ActionableNotificationProps {
  kind: NotificationKind;
  title: string;
  subtitle?: string;
  actionButtonLabel: string;
  onActionButtonClick: () => void;
  className?: string;
  lowContrast?: boolean;
  hideCloseButton?: boolean;
  inline?: boolean;
  onClose?: () => void;
}

/** Carbon ActionableNotification — notification with action button. */
export const ActionNotification: FC<ActionableNotificationProps> = ({
  kind,
  title,
  subtitle,
  actionButtonLabel,
  onActionButtonClick,
  className,
  lowContrast = true,
  hideCloseButton = true,
  inline = false,
  onClose,
}) => (
  <ActionableNotification
    kind={kind}
    title={title}
    subtitle={subtitle}
    actionButtonLabel={actionButtonLabel}
    onActionButtonClick={onActionButtonClick}
    lowContrast={lowContrast}
    hideCloseButton={hideCloseButton}
    inline={inline}
    onCloseButtonClick={onClose}
    className={cn('!max-w-full', className)}
  />
);

// ─── Link-style Actionable Notification ──────────────────────────────────────

export interface LinkNotificationProps {
  kind: NotificationKind;
  title: string;
  subtitle?: string;
  linkText: string;
  onLinkClick: () => void;
  className?: string;
  lowContrast?: boolean;
  hideCloseButton?: boolean;
}

/** Carbon notification with link-styled action button. */
export const LinkNotification: FC<LinkNotificationProps> = ({
  kind,
  title,
  subtitle,
  linkText,
  onLinkClick,
  className,
  lowContrast = true,
  hideCloseButton = true,
}) => (
  <ActionableNotification
    kind={kind}
    title={title}
    subtitle={subtitle}
    actionButtonLabel={linkText}
    onActionButtonClick={onLinkClick}
    lowContrast={lowContrast}
    hideCloseButton={hideCloseButton}
    inline
    className={cn('!max-w-full notice-link-style', className)}
  />
);
