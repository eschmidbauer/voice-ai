import { FC, HTMLAttributes } from 'react';
import { BaseCard, CardDescription, CardTitle } from '@/app/components/base/cards';
import { cn } from '@/utils';
import { AssistantTool } from '@rapidaai/react';
import { BUILDIN_TOOLS } from '@/llm-tools';
import { Tag, ButtonSet } from '@carbon/react';
import {
  PrimaryButton,
  DangerGhostButton,
} from '@/app/components/carbon/button';
import { Edit, TrashCan } from '@carbon/icons-react';

interface ToolCardProps extends HTMLAttributes<HTMLDivElement> {
  tool: AssistantTool;
  onEdit?: () => void;
  onDelete?: () => void;
  iconClass?: string;
  titleClass?: string;
  isConnected?: boolean;
}

export const SelectToolCard: FC<ToolCardProps> = ({
  tool,
  onEdit,
  onDelete,
  className,
}) => {
  const hasProtobufMethods = typeof tool.getExecutionmethod === 'function';
  const executionMethod = hasProtobufMethods ? tool.getExecutionmethod() : '';
  const isMCP = executionMethod === 'mcp';

  const toolName = hasProtobufMethods ? tool.getName?.() : (tool as any).name;
  const toolDescription = hasProtobufMethods
    ? tool.getDescription?.()
    : (tool as any).description;

  const toolMeta = BUILDIN_TOOLS.find(x => x.code === executionMethod);

  return (
    <BaseCard className={cn('flex flex-col', className)}>
      {/* Body */}
      <div className="p-4 flex-1 flex flex-col gap-3">
        {/* Header row: icon + tags */}
        <header className="flex items-start justify-between">
          <div className="w-9 h-9 flex items-center justify-center bg-gray-100 dark:bg-gray-800/60 shrink-0">
            {toolMeta?.icon ? (
              <img
                alt={toolMeta.name}
                src={toolMeta.icon}
                className="w-5 h-5 object-contain"
              />
            ) : (
              <span className="text-xs font-semibold text-gray-400 uppercase">
                {(toolName ?? '?').charAt(0)}
              </span>
            )}
          </div>
          <div className="flex items-center gap-1">
            {toolMeta && (
              <Tag type="gray" size="sm">{toolMeta.name}</Tag>
            )}
            {isMCP && (
              <Tag type="purple" size="sm">MCP</Tag>
            )}
            {!toolMeta && !isMCP && (
              <Tag type="gray" size="sm" className="capitalize">
                {executionMethod.replace(/_/g, ' ')}
              </Tag>
            )}
          </div>
        </header>

        {/* Name + description */}
        <div className="flex-1 flex flex-col gap-1 min-w-0">
          <CardTitle className="line-clamp-1 text-sm font-semibold">
            {toolName}
          </CardTitle>
          <CardDescription className="line-clamp-2 text-xs leading-relaxed">
            {toolDescription}
          </CardDescription>
        </div>
      </div>

      {/* Footer: action buttons */}
      <ButtonSet className="border-t border-gray-200 dark:border-gray-800 [&>button]:!flex-1 [&>button]:!max-w-none">
        {onDelete && (
          <DangerGhostButton size="md" renderIcon={TrashCan} onClick={onDelete}>
            Delete
          </DangerGhostButton>
        )}
        {onEdit && (
          <PrimaryButton size="md" renderIcon={Edit} onClick={onEdit}>
            Edit
          </PrimaryButton>
        )}
      </ButtonSet>
    </BaseCard>
  );
};
