import type { FC } from 'react';
import { Loading } from '@carbon/react';
import { cn } from '@/utils';

interface PageLoadingProps {
  className?: string;
}

/**
 * Page-level loading indicator using Carbon's Loading component.
 */
export const PageLoading: FC<PageLoadingProps> = ({ className }) => {
  return (
    <div
      className={cn(
        'flex items-center justify-center py-16',
        className,
      )}
    >
      <Loading description="Loading" withOverlay={false} small />
    </div>
  );
};
