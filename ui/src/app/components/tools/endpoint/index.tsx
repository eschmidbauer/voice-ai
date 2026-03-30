import { FC } from 'react';
import { Endpoint } from '@rapidaai/react';
import { cn } from '@/utils';
import { EndpointDropdown } from '@/app/components/dropdown/endpoint-dropdown';
import { InputGroup } from '@/app/components/input-group';
import {
  ConfigureToolProps,
  ToolDefinitionForm,
  ParameterEditor,
  PARAMETER_TYPE_OPTIONS,
  useParameterManager,
} from '../common';

// ============================================================================
// Constants
// ============================================================================

/** Endpoint tool does not expose the 'custom' parameter type. */
const ENDPOINT_TYPE_OPTIONS = PARAMETER_TYPE_OPTIONS.filter(
  o => o.value !== 'custom',
);

// ============================================================================
// Main Component
// ============================================================================

export const ConfigureEndpoint: FC<ConfigureToolProps> = ({
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
      <InputGroup title="Action Definition">
        <div className="flex flex-col gap-6 max-w-6xl">
          <EndpointDropdown
            className={cn('bg-light-background', inputClass)}
            currentEndpoint={getParamValue('tool.endpoint_id')}
            onChangeEndpoint={(endpoint: Endpoint) => {
              if (endpoint) {
                updateParameter('tool.endpoint_id', endpoint.getId());
              }
            }}
          />
          <ParameterEditor
            value={getParamValue('tool.parameters')}
            onChange={value => updateParameter('tool.parameters', value)}
            typeOptions={ENDPOINT_TYPE_OPTIONS}
            inputClass={inputClass}
          />
        </div>
      </InputGroup>

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
