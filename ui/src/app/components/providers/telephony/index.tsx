import { Metadata, VaultCredential } from '@rapidaai/react';
import { CredentialDropdown } from '@/app/components/dropdown/credential-dropdown';
import { useCallback } from 'react';
import { ProviderComponentProps } from '@/app/components/providers';
import { TELEPHONY_PROVIDER } from '@/providers';
import { Dropdown } from '@carbon/react';
import { Stack } from '@/app/components/carbon/form';
import { loadProviderConfig } from '@/providers/config-loader';
import { getDefaultsFromConfig, validateFromConfig } from '@/providers/config-defaults';
import { ConfigRenderer } from '@/app/components/providers/config-renderer';

const VONAGE_PHONE_REGEX = /^\+?[1-9]\d{1,14}$/;

export const GetDefaultTelephonyConfigIfInvalid = (
  provider: string,
  parameters: Metadata[],
): Metadata[] => {
  const config = loadProviderConfig(provider);
  if (!config?.telephony) return [];
  const normalized = getDefaultsFromConfig(
    config,
    'telephony',
    parameters,
    provider,
    { includeCredential: false },
  );
  const credentialValue =
    parameters.find(p => p.getKey() === 'rapida.credential_id')?.getValue() ??
    '';
  const credential = new Metadata();
  credential.setKey('rapida.credential_id');
  credential.setValue(credentialValue);
  return [credential, ...normalized];
};

export const ValidateTelephonyOptions = (
  provider: string,
  parameters: Metadata[],
): boolean => {
  const config = loadProviderConfig(provider);
  if (!config?.telephony) return false;

  const validationError = validateFromConfig(
    config,
    'telephony',
    provider,
    parameters,
  );
  if (validationError) return false;

  if (provider === 'vonage') {
    const phone = parameters.find(opt => opt.getKey() === 'phone')?.getValue();
    if (!phone || !VONAGE_PHONE_REGEX.test(phone)) return false;
  }

  return true;
};

export const ConfigureTelephonyComponent: React.FC<ProviderComponentProps> = ({
  provider,
  parameters,
  onChangeParameter,
}) => {
  const config = loadProviderConfig(provider);
  if (!config?.telephony) return null;

  return (
    <ConfigRenderer
      provider={provider}
      category="telephony"
      config={config.telephony}
      parameters={parameters}
      onParameterChange={onChangeParameter}
    />
  );
};

export const TelephonyProvider: React.FC<ProviderComponentProps> = props => {
  const { provider, onChangeParameter, onChangeProvider, parameters } = props;
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

  const selectedProvider =
    TELEPHONY_PROVIDER.find(x => x.code === provider) || null;

  return (
    <Stack gap={6}>
      <Dropdown
        id="telephony-provider"
        titleText="Telephony provider"
        label="Select telephony provider"
        items={TELEPHONY_PROVIDER}
        selectedItem={selectedProvider}
        itemToString={(item: any) => item?.name || ''}
        onChange={({ selectedItem }: any) => {
          if (!selectedItem) return;
          onChangeProvider(selectedItem.code);
          onChangeParameter(
            GetDefaultTelephonyConfigIfInvalid(selectedItem.code, []),
          );
        }}
        helperText="Select a telephony provider for inbound and outbound phone calls."
      />
      {provider && (
        <CredentialDropdown
          onChangeCredential={(c: VaultCredential) => {
            updateParameter('rapida.credential_id', c.getId());
          }}
          currentCredential={getParamValue('rapida.credential_id')}
          provider={provider}
        />
      )}
      {provider && (
        <div className="grid grid-cols-3 gap-x-6 gap-y-3">
          <ConfigureTelephonyComponent {...props} />
        </div>
      )}
    </Stack>
  );
};
