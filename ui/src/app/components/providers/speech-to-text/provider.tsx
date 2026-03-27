import { Metadata } from '@rapidaai/react';
import { ProviderComponentProps } from '@/app/components/providers';
import { loadProviderConfig } from '@/providers/config-loader';
import { getDefaultsFromConfig, validateFromConfig } from '@/providers/config-defaults';
import { ConfigRenderer } from '@/app/components/providers/config-renderer';
import { FC } from 'react';

export const GetDefaultSpeechToTextIfInvalid = (
  provider: string,
  parameters: Metadata[],
) => {
  const config = loadProviderConfig(provider);
  if (!config?.stt) return parameters;
  return getDefaultsFromConfig(config, 'stt', parameters, provider);
};

export const ValidateSpeechToTextIfInvalid = (
  provider: string,
  parameters: Metadata[],
  providerCredentialIds?: string[],
): string | undefined => {
  const config = loadProviderConfig(provider);
  if (!config?.stt) return undefined;
  const validationError = validateFromConfig(
    config,
    'stt',
    provider,
    parameters,
  );
  if (validationError) return validationError;

  if (!providerCredentialIds) return undefined;

  const credentialID = parameters.find(
    opt => opt.getKey() === 'rapida.credential_id',
  )?.getValue();
  if (!credentialID) {
    return `Please provide a valid ${provider} credential.`;
  }
  if (!providerCredentialIds.includes(credentialID)) {
    return `Please select a valid ${provider} credential.`;
  }

  return undefined;
};

/**
 *
 * @returns
 */
export const GetDefaultMicrophoneConfig = (
  existing: Metadata[] = [],
  defaults?: {
    'microphone.eos.fallback_timeout'?: string;
    'microphone.eos.provider'?: string;
    'microphone.denoising.provider'?: string;
    'microphone.vad.provider'?: string;
    'microphone.vad.threshold'?: string;
  },
): Metadata[] => {
  const defaultConfig = [
    {
      key: 'microphone.eos.fallback_timeout',
      value: defaults?.['microphone.eos.fallback_timeout'] ?? '700',
    },
    {
      key: 'microphone.eos.provider',
      value: defaults?.['microphone.eos.provider'] ?? 'pipecat_smart_turn_eos',
    },
    {
      key: 'microphone.denoising.provider',
      value: defaults?.['microphone.denoising.provider'] ?? 'rn_noise',
    },
    {
      key: 'microphone.vad.provider',
      value: defaults?.['microphone.vad.provider'] ?? 'silero_vad',
    },
    {
      key: 'microphone.vad.threshold',
      value: defaults?.['microphone.vad.threshold'] ?? '0.6',
    },
  ];

  const existingKeys = new Set(existing.map(m => m.getKey()));

  const newConfigs = defaultConfig
    .filter(({ key }) => !existingKeys.has(key))
    .map(({ key, value }) => {
      const metadata = new Metadata();
      metadata.setKey(key);
      metadata.setValue(value);
      return metadata;
    });

  return [...existing, ...newConfigs];
};

export const SpeechToTextConfigComponent: FC<ProviderComponentProps> = ({
  provider,
  parameters,
  onChangeParameter,
}) => {
  const config = loadProviderConfig(provider);
  if (!config?.stt) return null;
  return (
    <ConfigRenderer
      provider={provider}
      category="stt"
      config={config.stt}
      parameters={parameters}
      onParameterChange={onChangeParameter}
    />
  );
};
