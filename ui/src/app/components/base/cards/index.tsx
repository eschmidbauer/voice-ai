import { CustomLink, CustomLinkProps } from '@/app/components/custom-link';
import { MultiplePills } from '@/app/components/pill';
import { Tooltip } from '@/app/components/tooltip';
import { cn } from '@/utils';
import { FC, HTMLAttributes, ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { CornerBorderOverlay } from '@/app/components/base/corner-border';

// ─── Unified card primitives ───────────────────────────────────────────────

/** Static card — background + corner-bracket hover, no interactivity. */
export const BaseCard: FC<HTMLAttributes<HTMLDivElement>> = ({
  className,
  children,
  ...props
}) => (
  <div
    className={cn(
      'bg-white dark:bg-gray-950/20 border border-gray-200 dark:border-gray-800 relative group flex flex-col transition-colors duration-100',
      className,
    )}
    {...props}
  >
    <CornerBorderOverlay />
    {children}
  </div>
);

/** Navigable card — wraps content in a react-router Link. */
export const LinkCard: FC<{
  to: string;
  className?: string;
  children?: ReactNode;
}> = ({ to, className, children }) => (
  <Link
    to={to}
    className={cn(
      'bg-white dark:bg-gray-950/20 border border-gray-200 dark:border-gray-800 relative group flex flex-col transition-colors duration-100',
      className,
    )}
  >
    <CornerBorderOverlay />
    {children}
  </Link>
);

/** Actionable card — div with role="button", keyboard support, and focus ring. */
export const ActionCard: FC<HTMLAttributes<HTMLDivElement>> = ({
  className,
  children,
  onClick,
  onKeyDown,
  ...props
}) => {
  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Enter' || e.key === ' ')
      onClick?.(e as unknown as React.MouseEvent<HTMLDivElement>);
    onKeyDown?.(e);
  };

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={handleKeyDown}
      className={cn(
        'bg-white dark:bg-gray-950/20 border border-gray-200 dark:border-gray-800 relative group flex flex-col transition-colors duration-100 cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary',
        className,
      )}
      {...props}
    >
      <CornerBorderOverlay />
      {children}
    </div>
  );
};

// ─── Legacy card primitives (kept for backwards compatibility) ─────────────

interface CardProps extends HTMLAttributes<HTMLDivElement> {}
export const Card: FC<CardProps> = ({ children, className, ...props }) => {
  return (
    <div
      className={cn(
        // Carbon tile — no border radius, 1px border
        'dark:bg-gray-950 bg-white relative group flex flex-col overflow-hidden p-4 h-fit border rounded-none',
        className,
      )}
      {...props}
    >
      <CornerBorderOverlay />
      {children}
    </div>
  );
};

interface ClickableCardProps extends CardProps {}

export const ClickableCard: FC<ClickableCardProps & CustomLinkProps> = ({
  to,
  isExternal,
  children,
  className,
}) => {
  return (
    <CustomLink to={to} isExternal={isExternal}>
      {/* Carbon clickable tile: no shadow, hover = subtle bg tint */}
      <Card
        className={cn(
          'group cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-900/60 transition-colors duration-100',
          className,
        )}
      >
        {children}
      </Card>
    </CustomLink>
  );
};

interface CardTitleProps extends HTMLAttributes<HTMLDivElement> {
  status?: string;
  title?: string;
  children?: any;
}
export const CardTitle: FC<CardTitleProps> = ({
  title,
  status,
  children,
  className,
}) => {
  return (
    <div className={cn('capitalize', className)}>
      <span className="text-sm/6 font-medium">
        {title}
        {children}
      </span>
      {status === 'active' && (
        <Tooltip
          icon={
            <span className="relative flex h-2 w-2 ml-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-[2px] bg-blue-400 opacity-75"></span>
              <span className="relative inline-flex rounded-[2px] h-2 w-2 bg-blue-500"></span>
            </span>
          }
        >
          <p>Active and available to use right now</p>
        </Tooltip>
      )}
    </div>
  );
};
interface CardDescriptionProps extends HTMLAttributes<HTMLDivElement> {
  description?: string;
  children?: any;
}
export const CardDescription: FC<CardDescriptionProps> = ({
  description,
  className,
  children,
}) => {
  return (
    <p
      className={cn(
        'mt-1 opacity-70 text-sm leading-normal line-clamp-2',
        className,
      )}
    >
      {description}
      {children}
    </p>
  );
};

interface CardTagProps extends HTMLAttributes<HTMLDivElement> {
  tags?: string[];
}
export const CardTag: FC<CardTagProps> = ({ tags, className }) => {
  return (
    <MultiplePills
      tags={tags}
      className={cn('rounded-[2px] w-fit px-4 text-sm', className)}
    />
  );
};
