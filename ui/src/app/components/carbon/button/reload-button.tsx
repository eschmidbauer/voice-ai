import { IconOnlyButton } from '@/app/components/carbon/button';
import { Renew } from '@carbon/icons-react';
import type { FC, MouseEventHandler } from 'react';

interface ReloadButtonProps {
  onClick?: MouseEventHandler<HTMLButtonElement>;
  isLoading?: boolean;
  className?: string;
}

export const ReloadButton: FC<ReloadButtonProps> = ({ onClick, isLoading, className }) => {
  return (
    <IconOnlyButton
      kind="ghost"
      size="sm"
      renderIcon={Renew}
      iconDescription="Reload"
      onClick={onClick}
      disabled={isLoading}
      className={className}
    />
  );
};
