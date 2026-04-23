import { FC, useState, useCallback } from 'react';
import { Stack, TextArea } from '@/app/components/carbon/form';
import { Slider } from '@carbon/react';
import {
  ConfigureToolProps,
  ToolDefinitionForm,
  useParameterManager,
} from '../common';
import { InputGroup } from '../../input-group';
import ConfigSelect from '@/app/components/configuration/config-var/config-select';
import { SEPARATOR } from './constant';

export const ConfigureTransferCall: FC<ConfigureToolProps> = ({
  toolDefinition,
  onChangeToolDefinition,
  inputClass,
  parameters,
  onParameterChange,
}) => {
  const { getParamValue, updateParameter } = useParameterManager(
    parameters,
    onParameterChange,
  );

  const [transferToList, setTransferToList] = useState<string[]>(() => {
    const raw = getParamValue('tool.transfer_to');
    return raw ? raw.split(SEPARATOR) : [];
  });

  const handleTransferToChange = useCallback(
    (options: string[]) => {
      setTransferToList(options);
      updateParameter(
        'tool.transfer_to',
        options.filter(Boolean).join(SEPARATOR),
      );
    },
    [updateParameter],
  );

  return (
    <>
      <InputGroup title="Action Definition">
        <Stack gap={7}>
          <ConfigSelect
            options={transferToList}
            label="Add transfer destination"
            placeholder="+14155551234 or sip:agent@example.com"
            helperText="Phone numbers or SIP URIs to transfer calls to. Drag to reorder."
            onChange={handleTransferToChange}
          />
          <hr className="border-gray-200 dark:border-gray-800" />
          <TextArea
            id="transfer-message"
            labelText="Transfer Message"
            helperText="The message to be played when transferring the call."
            value={getParamValue('tool.transfer_message')}
            onChange={e =>
              updateParameter('tool.transfer_message', e.target.value)
            }
            placeholder="Your transfer message"
          />
          <Slider
            id="transfer-delay"
            labelText="Transfer Delay (ms)"
            min={0}
            max={1000}
            step={50}
            value={Number(getParamValue('tool.transfer_delay')) || 0}
            onChange={({ value }: { value: number }) =>
              updateParameter('tool.transfer_delay', value.toString())
            }
          />
        </Stack>
      </InputGroup>

      {toolDefinition && onChangeToolDefinition && (
        <ToolDefinitionForm
          toolDefinition={toolDefinition}
          onChangeToolDefinition={onChangeToolDefinition}
          inputClass={inputClass}
          documentationUrl="https://doc.rapida.ai/assistants/tools/add-transfer-call-tool"
          documentationTitle="Know more about Transfer Call that can be supported by rapida"
        />
      )}
    </>
  );
};
