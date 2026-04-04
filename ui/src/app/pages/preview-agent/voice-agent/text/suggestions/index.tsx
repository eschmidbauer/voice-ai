import { FC } from 'react';
import { GhostButton } from '@/app/components/carbon/button';
import { Chat } from '@carbon/icons-react';

export const QuickSuggestion: FC<{
  suggestion: string;
  onClick: () => void;
}> = ({ onClick, suggestion }) => {
  return (
    <GhostButton size="sm" renderIcon={Chat} onClick={onClick}>
      {suggestion}
    </GhostButton>
  );
};
