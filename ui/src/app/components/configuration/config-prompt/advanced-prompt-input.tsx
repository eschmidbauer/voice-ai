import type { FC } from 'react';
import React, { useRef } from 'react';
import { useBoolean } from 'ahooks';
import MessageTypeSelector from './message-type-selector';
import type { PromptRole } from '@/models/prompt';
import { DeleteIcon } from '@/app/components/Icon/delete';
import { GhostButton } from '@/app/components/carbon/button';
import { TickIcon } from '@/app/components/Icon/Tick';
import { CopyIcon } from '@/app/components/Icon/Copy';
import { cn } from '@/utils';
import { useToggleExpend } from '@/hooks/use-toggle-expend';
import PromptEditor from '@/app/components/prompt-editor';
import { Maximize2, Minimize2 } from 'lucide-react';
type PromptEditorProps = {
  type?: PromptRole;
  isChatMode: boolean;
  value: string;
  onTypeChange: (value: PromptRole) => void;
  onChange: (value: string) => void;
  canDelete: boolean;
  onDelete: () => void;
  className?: string;
  instanceId?: string;
  enableReservedVariableSuggestions?: boolean;
};

const AdvancedPromptInput: FC<PromptEditorProps> = ({
  type,
  value,
  onChange,
  onTypeChange,
  canDelete,
  onDelete,
  className,
  enableReservedVariableSuggestions = false,
}) => {
  // expand feature
  const ref = useRef<HTMLDivElement>(null);
  const { wrapClassName, isExpand, setIsExpand } = useToggleExpend(ref);

  //   focus
  const [isFocus, { setTrue: setFocus, setFalse: setBlur }] = useBoolean(false);

  //   is checked when copy
  const [isChecked, { setTrue: setChecked, setFalse: setUnCheck }] =
    useBoolean(false);

  const handlePromptChange = (newValue: string) => {
    if (value === newValue) return;
    onChange(newValue);
  };

  const copyItem = (item: string) => {
    setChecked();
    navigator.clipboard.writeText(item);
    setTimeout(() => {
      setUnCheck();
    }, 4000); // Reset back after 2 seconds
  };

  return (
    <div className={cn(wrapClassName)}>
      <div
        ref={ref}
        className={cn(
          'border-b border-gray-300 dark:border-gray-700',
          'transition-colors duration-100',
          'relative',
          'bg-light-background dark:bg-gray-950',
          isFocus && 'border-primary',
          // ::after overlay — renders above children so focus ring is fully visible
          'after:content-[""] after:absolute after:inset-0 after:pointer-events-none after:z-[1]',
          'after:outline-solid after:outline-[1.5px] after:outline-transparent after:outline-offset-[-1.5px]',
          'focus-within:after:outline-primary',
          isFocus && 'after:outline-primary!',
          isExpand ? 'h-full z-50' : '',
          className,
        )}
      >
        <div
          className={cn(
            'flex justify-between items-center',
            'border-b border-gray-300 dark:border-gray-700',
          )}
        >
          <MessageTypeSelector value={type} onChange={onTypeChange} />
          {canDelete && (
            <GhostButton
              size="md"
              onClick={onDelete}
              tabIndex={-1}
              className="hover:border-red-600  dark:hover:border-red-600 transition-colors border border-transparent border-l-gray-300 dark:border-l-gray-700"
            >
              <DeleteIcon className="w-4 h-4 text-red-600" />
            </GhostButton>
          )}
          <GhostButton
            size="md"
            tabIndex={-1}
            onClick={() => {
              copyItem(value);
            }}
            className="hover:border-blue-600  dark:hover:border-blue-600  transition-colors border border-transparent border-l-gray-300 dark:border-l-gray-700"
          >
            {isChecked ? (
              <TickIcon className="h-4 w-4 text-green-600" />
            ) : (
              <CopyIcon className="h-4 w-4" />
            )}
          </GhostButton>
          <GhostButton
            size="md"
            tabIndex={-1}
            onClick={() => {
              setIsExpand(!isExpand);
            }}
            className="hover:border-blue-600 dark:hover:border-blue-600  transition-colors border border-transparent border-l-gray-300 dark:border-l-gray-700"
          >
            {isExpand ? (
              <Minimize2 className="h-4 w-4" strokeWidth={1.5} />
            ) : (
              <Maximize2 className="h-4 w-4" strokeWidth={1.5} />
            )}
          </GhostButton>
        </div>

        <PromptEditor
          className={cn(
            'min-h-[200px]',
            isExpand ? 'py-2 px-4 h-screen' : 'py-1 px-2',
          )}
          placeholder={`Write your prompt here. enter {{variable}} to insert a variable.`}
          value={value}
          onFocus={setFocus}
          onChange={handlePromptChange}
          onBlur={setBlur}
          enableReservedVariableSuggestions={enableReservedVariableSuggestions}
        />
      </div>
    </div>
  );
};
export default React.memo(AdvancedPromptInput);
