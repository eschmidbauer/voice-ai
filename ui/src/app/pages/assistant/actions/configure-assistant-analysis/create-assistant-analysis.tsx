import React, { FC, useState } from 'react';
import { useConfirmDialog } from '@/app/pages/assistant/actions/hooks/use-confirmation';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import {
  PrimaryButton,
  SecondaryButton,
  TertiaryButton,
} from '@/app/components/carbon/button';
import { Stack, TextInput, TextArea } from '@/app/components/carbon/form';
import { ButtonSet, Select, SelectItem, Button, NumberInput } from '@carbon/react';
import { Add, TrashCan } from '@carbon/icons-react';
import { useCurrentCredential } from '@/hooks/use-credential';
import { randomMeaningfullName } from '@/utils';
import { EndpointDropdown } from '@/app/components/dropdown/endpoint-dropdown';
import { CreateAnalysis, Endpoint } from '@rapidaai/react';
import toast from 'react-hot-toast/headless';
import { connectionConfig } from '@/configs';
import { TabForm } from '@/app/components/form/tab-form';

// ── Parameter types ──────────────────────────────────────────────────────────

type ParamType = 'assistant' | 'conversation' | 'argument' | 'metadata' | 'option' | 'analysis';

interface Parameter {
  type: ParamType;
  key: string;
  value: string;
}

const PARAM_TYPE_OPTIONS = [
  { value: 'assistant', text: 'Assistant' },
  { value: 'conversation', text: 'Conversation' },
  { value: 'argument', text: 'Argument' },
  { value: 'metadata', text: 'Metadata' },
  { value: 'option', text: 'Option' },
  { value: 'analysis', text: 'Analysis' },
];

const ASSISTANT_KEYS = [
  { value: 'name', text: 'Name' },
  { value: 'prompt', text: 'Prompt' },
];

const CONVERSATION_KEYS = [
  { value: 'messages', text: 'Messages' },
];

// ── Parameter Editor (Carbon native) ─────────────────────────────────────────

const ParameterEditor: FC<{
  parameters: Parameter[];
  onChange: (params: Parameter[]) => void;
}> = ({ parameters, onChange }) => {
  const update = (index: number, field: keyof Parameter, value: string) => {
    onChange(parameters.map((p, i) => (i === index ? { ...p, [field]: value } : p)));
  };

  const remove = (index: number) => {
    onChange(parameters.filter((_, i) => i !== index));
  };

  const add = () => {
    onChange([...parameters, { type: 'assistant', key: '', value: '' }]);
  };

  const getKeyOptions = (type: ParamType) => {
    switch (type) {
      case 'assistant': return ASSISTANT_KEYS;
      case 'conversation': return CONVERSATION_KEYS;
      default: return null;
    }
  };

  return (
    <div>
      <p className="text-xs font-medium mb-2">Parameters ({parameters.length})</p>
      <div className="border border-gray-200 dark:border-gray-700 divide-y divide-gray-200 dark:divide-gray-700">
        {parameters.map((param, index) => {
          const keyOptions = getKeyOptions(param.type);
          return (
            <div
              key={index}
              className="flex items-center gap-2 px-2 py-1.5"
            >
              <Select
                id={`param-type-${index}`}
                labelText=""
                hideLabel
                value={param.type}
                onChange={e => {
                  update(index, 'type', e.target.value);
                  update(index, 'key', '');
                }}
                className="!min-w-[120px] flex-shrink-0"
              >
                {PARAM_TYPE_OPTIONS.map(o => (
                  <SelectItem key={o.value} value={o.value} text={o.text} />
                ))}
              </Select>
              {keyOptions ? (
                <Select
                  id={`param-key-${index}`}
                  labelText=""
                  hideLabel
                  value={param.key}
                  onChange={e => update(index, 'key', e.target.value)}
                  className="flex-1"
                >
                  <SelectItem value="" text="Select key" />
                  {keyOptions.map(o => (
                    <SelectItem key={o.value} value={o.value} text={o.text} />
                  ))}
                </Select>
              ) : (
                <TextInput
                  id={`param-key-${index}`}
                  labelText=""
                  hideLabel
                  value={param.key}
                  onChange={e => update(index, 'key', e.target.value)}
                  placeholder="Source key"
                  size="md"
                  className="flex-1"
                />
              )}
              <span className="text-xs text-gray-400 shrink-0 px-1">as</span>
              <TextInput
                id={`param-value-${index}`}
                labelText=""
                hideLabel
                value={param.value}
                onChange={e => update(index, 'value', e.target.value)}
                placeholder="Variable name"
                size="md"
                className="flex-1"
              />
              <Button
                hasIconOnly
                renderIcon={TrashCan}
                iconDescription="Remove"
                kind="danger--ghost"
                size="md"
                onClick={() => remove(index)}
              />
            </div>
          );
        })}
      </div>
      <div className="pt-4">
        <TertiaryButton
          size="md"
          renderIcon={Add}
          onClick={add}
          className="!w-full !max-w-none"
        >
          Add parameter
        </TertiaryButton>
      </div>
    </div>
  );
};

// ── Main component ───────────────────────────────────────────────────────────

export const CreateAssistantAnalysis: FC<{ assistantId: string }> = ({
  assistantId,
}) => {
  const navigator = useGlobalNavigation();
  const { authId, token, projectId } = useCurrentCredential();
  const { showDialog, ConfirmDialogComponent } = useConfirmDialog({});

  const [activeTab, setActiveTab] = useState('configure');
  const [errorMessage, setErrorMessage] = useState('');
  const [name, setName] = useState(randomMeaningfullName('analysis'));
  const [description, setDescription] = useState('');
  const [priority, setPriority] = useState<number>(0);
  const [endpointId, setEndpointId] = useState<string>('');
  const [parameters, setParameters] = useState<Parameter[]>([
    { type: 'conversation', key: 'messages', value: 'messages' },
  ]);

  const validateConfigure = (): boolean => {
    setErrorMessage('');
    if (!endpointId) {
      setErrorMessage('Please select a valid endpoint to be executed for analysis.');
      return false;
    }
    if (parameters.length === 0) {
      setErrorMessage('Please provide one or more parameters.');
      return false;
    }
    const keys = parameters.map(p => `${p.type}.${p.key}`);
    if (new Set(keys).size !== keys.length) {
      setErrorMessage('Duplicate parameter keys are not allowed.');
      return false;
    }
    if (parameters.some(p => !p.key.trim() || !p.value.trim())) {
      setErrorMessage('Empty parameter keys or values are not allowed.');
      return false;
    }
    const values = parameters.map(p => p.value.trim());
    if (new Set(values).size !== values.length) {
      setErrorMessage('Duplicate parameter values are not allowed.');
      return false;
    }
    return true;
  };

  const onSubmit = () => {
    setErrorMessage('');
    if (!name) { setErrorMessage('Please provide a valid name.'); return; }

    CreateAnalysis(
      connectionConfig,
      assistantId,
      name,
      endpointId,
      'latest',
      priority,
      parameters.map(p => ({ key: `${p.type}.${p.key}`, value: p.value })),
      (err, response) => {
        if (err) { setErrorMessage('Unable to create analysis. Please try again.'); return; }
        if (response?.getSuccess()) {
          toast.success('Analysis added to assistant successfully');
          navigator.goToConfigureAssistantAnalysis(assistantId);
        } else {
          setErrorMessage(response?.getError()?.getHumanmessage() || 'Unable to create analysis.');
        }
      },
      { 'x-auth-id': authId, authorization: token, 'x-project-id': projectId },
      description,
    );
  };

  return (
    <>
      <ConfirmDialogComponent />
      <TabForm
        formHeading="Complete all steps to configure your analysis."
        activeTab={activeTab}
        onChangeActiveTab={() => {}}
        errorMessage={errorMessage}
        form={[
          {
            code: 'configure',
            name: 'Configure',
            description: 'Select the endpoint and map data parameters for analysis.',
            actions: [
              <ButtonSet className="!w-full [&>button]:!flex-1 [&>button]:!max-w-none">
                <SecondaryButton size="lg" onClick={() => showDialog(navigator.goBack)}>
                  Cancel
                </SecondaryButton>
                <PrimaryButton size="lg" onClick={() => { if (validateConfigure()) setActiveTab('profile'); }}>
                  Continue
                </PrimaryButton>
              </ButtonSet>,
            ],
            body: (
              <div className="px-8 pt-6 pb-8 max-w-4xl">
                <Stack gap={7}>
                  <EndpointDropdown
                    currentEndpoint={endpointId}
                    onChangeEndpoint={(e: Endpoint) => { if (e) setEndpointId(e.getId()); }}
                  />
                  <ParameterEditor parameters={parameters} onChange={setParameters} />
                </Stack>
              </div>
            ),
          },
          {
            code: 'profile',
            name: 'Profile',
            description: 'Provide a name and set the execution priority.',
            actions: [
              <ButtonSet className="!w-full [&>button]:!flex-1 [&>button]:!max-w-none">
                <SecondaryButton size="lg" onClick={() => showDialog(navigator.goBack)}>
                  Cancel
                </SecondaryButton>
                <PrimaryButton size="lg" onClick={onSubmit}>
                  Configure analysis
                </PrimaryButton>
              </ButtonSet>,
            ],
            body: (
              <div className="px-8 pt-6 pb-8 max-w-2xl">
                <Stack gap={6}>
                  <TextInput
                    id="analysis-name"
                    labelText="Name"
                    value={name}
                    onChange={e => setName(e.target.value)}
                    placeholder="A name for your analysis"
                    helperText="A unique name to identify this analysis configuration."
                  />
                  <TextArea
                    id="analysis-description"
                    labelText="Description (Optional)"
                    value={description}
                    onChange={e => setDescription(e.target.value)}
                    placeholder="An optional description of this analysis..."
                    rows={2}
                  />
                  <NumberInput
                    id="analysis-priority"
                    label="Execution Priority"
                    min={0}
                    value={priority}
                    onChange={(e: any, { value }: any) => setPriority(value)}
                    helperText="Lower numbers execute first when multiple analyses are triggered."
                  />
                </Stack>
              </div>
            ),
          },
        ]}
      />
    </>
  );
};
