import { IconOnlyButton } from '@/app/components/carbon/button';
import { Edit } from '@carbon/icons-react';
import type { FC, MouseEventHandler } from 'react';

interface EditButtonProps {
  onClick?: MouseEventHandler<HTMLButtonElement>;
  className?: string;
  disabled?: boolean;
}

export const EditButton: FC<EditButtonProps> = ({ onClick, className, disabled }) => {
  return (
    <IconOnlyButton
      kind="ghost"
      size="sm"
      renderIcon={Edit}
      iconDescription="Edit"
      onClick={onClick}
      disabled={disabled}
      className={className}
    />
  );
};
