import { FC, useState, useCallback } from 'react';
import { cn } from '@/utils';
import { CodeEditor } from '@/app/components/form/editor/code-editor';
import { DocNoticeBlock } from '@/app/components/container/message/notice-block/doc-notice-block';
import { Add, TrashCan, ArrowRight } from '@carbon/icons-react';
import { TertiaryButton } from '@/app/components/carbon/button';
import { Stack, TextInput, TextArea } from '@/app/components/carbon/form';
import { Select, SelectItem, Button } from '@carbon/react';
import {
  ToolDefinition,
  ParameterType,
  KeyValueParameter,
  PARAMETER_TYPE_OPTIONS,
  ASSISTANT_KEY_OPTIONS,
  CONVERSATION_KEY_OPTIONS,
  TOOL_KEY_OPTIONS,
} from './types';
import { parseJsonParameters, stringifyParameters } from './hooks';

// ============================================================================
// Documentation Notice Block
// ============================================================================

interface DocumentationNoticeProps {
  title?: string;
  documentationUrl: string;
}

export const DocumentationNotice: FC<DocumentationNoticeProps> = ({
  title = 'Know more about knowledge tool definition that can be supported by rapida',
  documentationUrl,
}) => (
  <DocNoticeBlock docUrl={documentationUrl}>{title}</DocNoticeBlock>
);

// ============================================================================
// Tool Definition Form
// ============================================================================

interface ToolDefinitionFormProps {
  toolDefinition: ToolDefinition;
  onChangeToolDefinition: (value: ToolDefinition) => void;
  inputClass?: string;
  documentationUrl?: string;
  documentationTitle?: string;
}

export const ToolDefinitionForm: FC<ToolDefinitionFormProps> = ({
  toolDefinition,
  onChangeToolDefinition,
  inputClass,
  documentationUrl = 'https://doc.rapida.ai/assistants/overview',
  documentationTitle,
}) => {
  return (
    <div>
      <DocumentationNotice
        title={documentationTitle}
        documentationUrl={documentationUrl}
      />
      <div className="px-6 pb-6 mt-4 max-w-6xl">
        <Stack gap={6}>
          <TextInput
            id="tool-def-name"
            labelText="Name"
            value={toolDefinition.name}
            onChange={e =>
              onChangeToolDefinition({ ...toolDefinition, name: e.target.value })
            }
            placeholder="Enter tool name"
          />
          <TextArea
            id="tool-def-description"
            labelText="Description"
            value={toolDefinition.description}
            onChange={e =>
              onChangeToolDefinition({ ...toolDefinition, description: e.target.value })
            }
            placeholder="A tool description or definition of when this tool will get triggered."
            rows={2}
          />
          <CodeEditor
            labelText="Parameters"
            placeholder="Provide tool parameters as JSON that will be passed to LLM"
            value={toolDefinition.parameters}
            onChange={value =>
              onChangeToolDefinition({ ...toolDefinition, parameters: value })
            }
          />
        </Stack>
      </div>
    </div>
  );
};

// ============================================================================
// Type Key Selector
// ============================================================================

interface TypeKeySelectorProps {
  type: ParameterType;
  value: string;
  onChange: (newValue: string) => void;
  inputClass?: string;
}

export const TypeKeySelector: FC<TypeKeySelectorProps> = ({
  type,
  value,
  onChange,
  inputClass,
}) => {
  const options = (() => {
    switch (type) {
      case 'assistant': return ASSISTANT_KEY_OPTIONS;
      case 'conversation': return CONVERSATION_KEY_OPTIONS;
      case 'tool': return TOOL_KEY_OPTIONS;
      default: return null;
    }
  })();

  if (options) {
    return (
      <Select
        id={`key-${type}-${value}`}
        labelText=""
        hideLabel
        value={value}
        onChange={e => onChange(e.target.value)}
        className={cn('flex-1', inputClass)}
      >
        {options.map(opt => (
          <SelectItem key={opt.value} value={opt.value} text={opt.name} />
        ))}
      </Select>
    );
  }

  return (
    <TextInput
      id={`key-custom-${value}`}
      labelText=""
      hideLabel
      value={value}
      onChange={e => onChange(e.target.value)}
      placeholder="Key"
      size="md"
    />
  );
};

// ============================================================================
// Parameter Editor
// ============================================================================

interface ParameterEditorProps {
  value: string;
  onChange: (value: string) => void;
  typeOptions?: Array<{ name: string; value: string }>;
  defaultNewType?: string;
  inputClass?: string;
}

export const ParameterEditor: FC<ParameterEditorProps> = ({
  value,
  onChange,
  typeOptions = [...PARAMETER_TYPE_OPTIONS],
  defaultNewType = 'assistant',
}) => {
  const [params, setParams] = useState<KeyValueParameter[]>(() =>
    parseJsonParameters(value),
  );

  const commit = useCallback(
    (next: KeyValueParameter[]) => {
      setParams(next);
      onChange(stringifyParameters(next));
    },
    [onChange],
  );

  const handleTypeChange = useCallback(
    (index: number, newType: string) => {
      const next = [...params];
      next[index] = { key: `${newType}.`, value: '' };
      commit(next);
    },
    [params, commit],
  );

  const handleKeyChange = useCallback(
    (index: number, newKey: string) => {
      const next = [...params];
      const [type] = params[index].key.split('.');
      next[index] = { ...params[index], key: `${type}.${newKey}` };
      commit(next);
    },
    [params, commit],
  );

  const handleValueChange = useCallback(
    (index: number, newValue: string) => {
      const next = [...params];
      next[index] = { ...params[index], value: newValue };
      commit(next);
    },
    [params, commit],
  );

  const handleRemove = useCallback(
    (index: number) => {
      commit(params.filter((_, i) => i !== index));
    },
    [params, commit],
  );

  const handleAdd = useCallback(() => {
    commit([...params, { key: `${defaultNewType}.`, value: '' }]);
  }, [params, commit, defaultNewType]);

  return (
    <div>
      <p className="text-xs font-medium mb-2">Mapping ({params.length})</p>
      <table className="w-full border-collapse border border-gray-200 dark:border-gray-700 text-sm [&_input]:!border-none [&_.cds--text-input]:!border-none [&_.cds--text-input]:!outline-none [&_.cds--select-input]:!border-none [&_.cds--form-item]:!m-0">
        <thead>
          <tr className="bg-gray-50 dark:bg-gray-900">
            <th className="text-left text-xs font-medium text-gray-500 dark:text-gray-400 px-3 py-2 border-b border-r border-gray-200 dark:border-gray-700 w-1/4">Type</th>
            <th className="text-left text-xs font-medium text-gray-500 dark:text-gray-400 px-3 py-2 border-b border-r border-gray-200 dark:border-gray-700 w-1/4">Key</th>
            <th className="border-b border-r border-gray-200 dark:border-gray-700 w-8" />
            <th className="text-left text-xs font-medium text-gray-500 dark:text-gray-400 px-3 py-2 border-b border-r border-gray-200 dark:border-gray-700 w-1/4">Value</th>
            <th className="border-b border-gray-200 dark:border-gray-700 w-10" />
          </tr>
        </thead>
        <tbody>
          {params.map(({ key, value: val }, index) => {
            const [type, pk] = key.split('.');
            return (
              <tr key={index} className="border-b border-gray-200 dark:border-gray-700 last:border-b-0">
                <td className="border-r border-gray-200 dark:border-gray-700 p-0">
                  <Select id={`param-type-${index}`} labelText="" hideLabel value={type} onChange={e => handleTypeChange(index, e.target.value)} size="md">
                    {typeOptions.map(opt => (
                      <SelectItem key={opt.value} value={opt.value} text={opt.name} />
                    ))}
                  </Select>
                </td>
                <td className="border-r border-gray-200 dark:border-gray-700 p-0">
                  <TypeKeySelector type={type as ParameterType} value={pk} onChange={newKey => handleKeyChange(index, newKey)} />
                </td>
                <td className="border-r border-gray-200 dark:border-gray-700 p-0 text-center text-gray-400">
                  <ArrowRight size={16} className="mx-auto" />
                </td>
                <td className="border-r border-gray-200 dark:border-gray-700 p-0">
                  <TextInput id={`param-val-${index}`} labelText="" hideLabel value={val} onChange={e => handleValueChange(index, e.target.value)} placeholder="Value" size="md" />
                </td>
                <td className="p-0 text-center">
                  <Button hasIconOnly renderIcon={TrashCan} iconDescription="Remove" kind="danger--ghost" size="sm" onClick={() => handleRemove(index)} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <div className="pt-4">
        <TertiaryButton
          size="md"
          renderIcon={Add}
          onClick={handleAdd}
          className="!w-full !max-w-none"
        >
          Add parameter
        </TertiaryButton>
      </div>
    </div>
  );
};
