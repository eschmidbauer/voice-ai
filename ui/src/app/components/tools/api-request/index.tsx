import { FC } from 'react';
import { Select, SelectItem } from '@carbon/react';
import { TextInput } from '@/app/components/carbon/form';
import { APiStringHeader } from '@/app/components/external-api/api-header';
import {
  ConfigureToolProps,
  ToolDefinitionForm,
  ParameterEditor,
  useParameterManager,
  HTTP_METHOD_OPTIONS,
} from '../common';

// ============================================================================
// Main Component
// ============================================================================

export const ConfigureAPIRequest: FC<ConfigureToolProps> = ({
  toolDefinition,
  onChangeToolDefinition,
  onParameterChange,
  parameters,
  inputClass,
}) => {
  const { getParamValue, updateParameter } = useParameterManager(
    parameters,
    onParameterChange,
  );

  return (
    <>
      <div className="px-6 pb-6">
        <div className="flex flex-col gap-6 max-w-6xl">
          <div className="flex space-x-2">
            <div className="relative w-40">
              <Select
                id="api-request-method"
                labelText="Method"
                value={getParamValue('tool.method')}
                onChange={e => updateParameter('tool.method', e.target.value)}
              >
                {HTTP_METHOD_OPTIONS.map(o => (
                  <SelectItem key={o.value} value={o.value} text={o.name} />
                ))}
              </Select>
            </div>
            <div className="relative w-full">
              <TextInput
                id="api-request-server-url"
                labelText="Server URL"
                value={getParamValue('tool.endpoint')}
                onChange={e => updateParameter('tool.endpoint', e.target.value)}
                placeholder="https://your-domain.com/api/v1/resource"
              />
            </div>
          </div>

          <div>
            <p className="text-xs font-medium mb-2">Headers</p>
            <APiStringHeader
              inputClass={inputClass}
              headerValue={getParamValue('tool.headers')}
              setHeaderValue={value => updateParameter('tool.headers', value)}
            />
          </div>

          <ParameterEditor
            value={getParamValue('tool.parameters')}
            onChange={value => updateParameter('tool.parameters', value)}
            inputClass={inputClass}
          />
        </div>
      </div>

      {toolDefinition && onChangeToolDefinition && (
        <ToolDefinitionForm
          toolDefinition={toolDefinition}
          onChangeToolDefinition={onChangeToolDefinition}
          inputClass={inputClass}
        />
      )}
    </>
  );
};
