import { Metadata, VaultCredential } from '@rapidaai/react';
import { ProviderComponentProps } from '@/app/components/providers';
import { loadProviderConfig } from '@/providers/config-loader';
import { getDefaultsFromConfig, validateFromConfig } from '@/providers/config-defaults';
import { ConfigRenderer } from '@/app/components/providers/config-renderer';
import { FC, useCallback, useMemo } from 'react';
import { CredentialDropdown } from '@/app/components/dropdown/credential-dropdown';
import { TEXT_PROVIDERS } from '@/providers';
import { NormalizeTextProviderModelSelection } from './model-normalization';
import { Dropdown } from '@carbon/react';

export const GetDefaultTextProviderConfigIfInvalid = (
  provider: string,
  parameters: Metadata[],
): Metadata[] => {
  const config = loadProviderConfig(provider);
  if (!config?.text) return parameters;
  const normalizedParameters = NormalizeTextProviderModelSelection(provider, parameters);
  return getDefaultsFromConfig(config, 'text', normalizedParameters, provider);
};

export const ValidateTextProviderDefaultOptions = (
  provider: string,
  parameters: Metadata[],
  providerCredentialIds?: string[],
): string | undefined => {
  const config = loadProviderConfig(provider);
  if (!config?.text) return 'Please select a valid model and provider.';
  const normalizedParameters = NormalizeTextProviderModelSelection(provider, parameters);
  const validationError = validateFromConfig(config, 'text', provider, normalizedParameters);
  if (validationError) return validationError;

  if (!providerCredentialIds) return undefined;
  const credentialID = normalizedParameters.find(
    opt => opt.getKey() === 'rapida.credential_id',
  )?.getValue();
  if (!credentialID) return `Please provide a valid ${provider} credential.`;
  if (!providerCredentialIds.includes(credentialID)) return `Please select a valid ${provider} credential.`;
  return undefined;
};

const TextProviderConfigComponent: FC<ProviderComponentProps> = ({
  provider,
  parameters,
  onChangeParameter,
}) => {
  const config = loadProviderConfig(provider);
  if (!config?.text) return null;
  return (
    <ConfigRenderer
      provider={provider}
      category="text"
      config={config.text}
      parameters={parameters}
      onParameterChange={onChangeParameter}
    />
  );
};

export const TextProvider: React.FC<ProviderComponentProps> = props => {
  const { provider, parameters, onChangeProvider, onChangeParameter } = props;
  const textProviders = useMemo(
    () => TEXT_PROVIDERS.filter(p => Boolean(loadProviderConfig(p.code)?.text)),
    [],
  );

  const getParamValue = useCallback(
    (key: string) =>
      parameters?.find(p => p.getKey() === key)?.getValue() ?? '',
    [parameters],
  );

  const updateParameter = (key: string, value: string) => {
    const updatedParams = [...(parameters || [])];
    const existingIndex = updatedParams.findIndex(p => p.getKey() === key);
    const newParam = new Metadata();
    newParam.setKey(key);
    newParam.setValue(value);
    if (existingIndex >= 0) {
      updatedParams[existingIndex] = newParam;
    } else {
      updatedParams.push(newParam);
    }
    onChangeParameter(updatedParams);
  };

  const selectedProvider = textProviders.find(x => x.code === provider) || null;

  return (
    <>
      <div className="flex items-stretch border border-gray-200 dark:border-gray-700">
        <div className="w-48 shrink-0 border-r border-gray-200 dark:border-gray-700">
          <Dropdown
            id="text-provider"
            titleText=""
            hideLabel
            label="Select provider"
            size="md"
            items={textProviders}
            selectedItem={selectedProvider}
            itemToString={(item: any) => item?.name || ''}
            onChange={({ selectedItem }: any) => {
              if (selectedItem) onChangeProvider(selectedItem.code);
            }}
          />
        </div>
        <TextProviderConfigComponent {...props} />
      </div>
      {provider && (
        <CredentialDropdown
          onChangeCredential={(c: VaultCredential) => {
            updateParameter('rapida.credential_id', c.getId());
          }}
          provider={provider}
          currentCredential={getParamValue('rapida.credential_id')}
        />
      )}
    </>
  );
};
