import type { FC, ReactNode } from 'react';
import { SkeletonText } from '@carbon/react';
import { cn } from '@/utils';

// ─── Types ───────────────────────────────────────────────────────────────────

export interface CarbonTextProps {
  children: ReactNode;
  className?: string;
  isLoading?: boolean;
  /** Show skeleton as a heading block (taller). */
  heading?: boolean;
  /** Width of the skeleton placeholder (e.g. "60%", "200px"). */
  skeletonWidth?: string;
  /** Number of skeleton lines when paragraph mode. */
  lineCount?: number;
  /** Render as a specific HTML element. Default: span. */
  as?: 'span' | 'p' | 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6' | 'div' | 'label';
}

/** Carbon Text — renders children or a SkeletonText placeholder when loading. */
export const Text: FC<CarbonTextProps> = ({
  children,
  className,
  isLoading = false,
  heading = false,
  skeletonWidth = '75%',
  lineCount = 1,
  as: Tag = 'span',
}) => {
  if (isLoading) {
    return (
      <SkeletonText
        heading={heading}
        width={skeletonWidth}
        lineCount={lineCount}
        paragraph={lineCount > 1}
        className={cn('!mb-0', className)}
      />
    );
  }

  return <Tag className={className}>{children}</Tag>;
};
