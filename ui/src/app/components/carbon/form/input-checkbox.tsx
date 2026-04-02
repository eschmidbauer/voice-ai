import { cn } from '@/utils';
import { Checkbox as CarbonCheckbox } from '@carbon/react';
import React, {
  ChangeEvent,
  forwardRef,
  InputHTMLAttributes,
  useId,
} from 'react';

interface InputCheckboxProps extends InputHTMLAttributes<HTMLInputElement> {
  children?: React.ReactNode;
}

export const InputCheckbox = forwardRef<HTMLInputElement, InputCheckboxProps>(
  ({ children, className, id, name, onChange, ...rest }, ref) => {
    const generatedId = useId().replace(/:/g, '');
    const checkboxId = id ?? name ?? `checkbox-${generatedId}`;

    return (
      <CarbonCheckbox
        {...rest}
        ref={ref}
        id={checkboxId}
        name={name}
        className={cn(className)}
        labelText={children ?? ''}
        hideLabel={!children}
        onChange={e => onChange?.(e as ChangeEvent<HTMLInputElement>)}
      />
    );
  },
);
