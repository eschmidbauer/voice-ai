import { IconOnlyButton } from '@/app/components/carbon/button';
import { TrashCan } from '@carbon/icons-react';
import type { FC, MouseEventHandler } from 'react';

interface DeleteButtonProps {
  onClick?: MouseEventHandler<HTMLButtonElement>;
  className?: string;
  disabled?: boolean;
}

export const DeleteButton: FC<DeleteButtonProps> = ({
  onClick,
  className,
  disabled,
}) => {
  return (
    <IconOnlyButton
      kind="ghost"
      size="sm"
      renderIcon={TrashCan}
      iconDescription="Delete"
      onClick={onClick}
      disabled={disabled}
      className={className}
    />
  );
};
