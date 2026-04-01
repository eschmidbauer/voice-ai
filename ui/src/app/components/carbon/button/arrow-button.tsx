import { PrimaryButton, SecondaryButton } from '@/app/components/carbon/button';
import { ArrowRight } from '@carbon/icons-react';
import { useRapidaStore } from '@/hooks';
import { cn } from '@/utils';
import type { FC, ButtonHTMLAttributes } from 'react';

interface ArrowButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  label: string;
  loading?: boolean;
}

export const ArrowButton: FC<ArrowButtonProps> = ({
  label,
  loading,
  disabled,
  className,
  ...props
}) => {
  const { isBlocking } = useRapidaStore();

  return (
    <PrimaryButton
      size="md"
      {...props}
      disabled={loading || disabled || isBlocking()}
      isLoading={loading || isBlocking()}
      renderIcon={ArrowRight}
      className={cn(isBlocking() ? 'opacity-80' : 'opacity-100', className)}
    >
      {label}
    </PrimaryButton>
  );
};

export const ArrowBorderButton: FC<ArrowButtonProps> = ({
  label,
  loading,
  disabled,
  className,
  ...props
}) => {
  const { isBlocking } = useRapidaStore();

  return (
    <SecondaryButton
      size="md"
      {...props}
      disabled={loading || disabled || isBlocking()}
      isLoading={loading || isBlocking()}
      renderIcon={ArrowRight}
      className={cn(isBlocking() ? 'opacity-80' : 'opacity-100', className)}
    >
      {label}
    </SecondaryButton>
  );
};
