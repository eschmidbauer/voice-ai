/**
 * Rapida – Open Source Voice AI Orchestration Platform
 * Copyright (C) 2023-2025 Prashant Srivastav <prashant@rapida.ai>
 * Licensed under a modified GPL-2.0. See the LICENSE file for details.
 */
import { Metadata } from '@rapidaai/react';
import { FC } from 'react';
import {
  ConfigureAzureTextToSpeech,
  GetAzureDefaultOptions,
  ValidateAzureOptions,
} from '@/app/components/providers/text-to-speech/azure-speech-service';
import {
  ConfigureCartesiaTextToSpeech,
  GetCartesiaDefaultOptions,
  ValidateCartesiaOptions,
} from '@/app/components/providers/text-to-speech/cartesia';
import {
  ConfigureDeepgramTextToSpeech,
  GetDeepgramDefaultOptions,
} from '@/app/components/providers/text-to-speech/deepgram';
import { ValidateDeepgramOptions } from '@/app/components/providers/text-to-speech/deepgram/constant';
import {
  ConfigureElevanLabTextToSpeech,
  GetElevanLabDefaultOptions,
  ValidateElevanLabOptions,
} from '@/app/components/providers/text-to-speech/elevenlabs';
import {
  ConfigureGoogleTextToSpeech,
  GetGoogleDefaultOptions,
  ValidateGoogleOptions,
} from '@/app/components/providers/text-to-speech/google-speech-service';
import {
  ConfigureOpenAITextToSpeech,
  GetOpenAIDefaultOptions,
  ValidateOpenAIOptions,
} from '@/app/components/providers/text-to-speech/openai';
import {
  ConfigurePlayhtTextToSpeech,
  GetPlayHTDefaultOptions,
  ValidatePlayHTOptions,
} from '@/app/components/providers/text-to-speech/playht';
import { ProviderComponentProps } from '@/app/components/providers';
import {
  ConfigureRimeTextToSpeech,
  GetRimeDefaultOptions,
  ValidateRimeOptions,
} from '@/app/components/providers/text-to-speech/rime';
import {
  ConfigureSarvamTextToSpeech,
  GetSarvamDefaultOptions,
  ValidateSarvamOptions,
} from '@/app/components/providers/text-to-speech/sarvam';

/**
 *
 * @returns
 */
export const GetDefaultSpeakerConfig = (
  existing: Metadata[] = [],
): Metadata[] => {
  const defaultConfig = [
    {
      key: 'speaker.conjunction.boundaries',
      value: '',
    },
    {
      key: 'speaker.conjunction.break',
      value: '240',
    },
    {
      key: 'speaker.pronunciation.dictionaries',
      value: '',
    },
  ];

  const result = [...existing];
  defaultConfig.forEach(item => {
    if (!existing.some(e => e.getKey() === item.key)) {
      const metadata = new Metadata();
      metadata.setKey(item.key);
      metadata.setValue(item.value);
      result.push(metadata);
    }
  });
  return result;
};

export const GetDefaultTextToSpeechIfInvalid = (
  provider: string,
  parameters: Metadata[],
): Metadata[] => {
  switch (provider) {
    case 'google-speech-service':
      return GetGoogleDefaultOptions(parameters);
    case 'elevenlabs':
      return GetElevanLabDefaultOptions(parameters);
    case 'playht':
      return GetPlayHTDefaultOptions(parameters);
    case 'deepgram':
      return GetDeepgramDefaultOptions(parameters);
    case 'openai':
      return GetOpenAIDefaultOptions(parameters);
    case 'azure-speech-service':
      return GetAzureDefaultOptions(parameters);
    case 'cartesia':
      return GetCartesiaDefaultOptions(parameters);
    case 'sarvamai':
      return GetSarvamDefaultOptions(parameters);
    case 'rime':
      return GetRimeDefaultOptions(parameters);
    default:
      return parameters;
  }
};

export const ValidateTextToSpeechIfInvalid = (
  provider: string,
  parameters: Metadata[],
): string | undefined => {
  switch (provider) {
    case 'google-speech-service':
      return ValidateGoogleOptions(parameters);
    case 'elevenlabs':
      return ValidateElevanLabOptions(parameters);
    case 'playht':
      return ValidatePlayHTOptions(parameters);
    case 'deepgram':
      return ValidateDeepgramOptions(parameters);
    case 'openai':
      return ValidateOpenAIOptions(parameters);
    case 'azure-speech-service':
      return ValidateAzureOptions(parameters);
    case 'cartesia':
      return ValidateCartesiaOptions(parameters);
    case 'sarvamai':
      return ValidateSarvamOptions(parameters);
    case 'rime':
      return ValidateRimeOptions(parameters);
    default:
      return undefined;
  }
};

export const TextToSpeechConfigComponent: FC<ProviderComponentProps> = ({
  provider,
  parameters,
  onChangeParameter,
}) => {
  switch (provider) {
    case 'google-speech-service':
      return (
        <ConfigureGoogleTextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    case 'elevenlabs':
      return (
        <ConfigureElevanLabTextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    case 'playht':
      return (
        <ConfigurePlayhtTextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    case 'deepgram':
      return (
        <ConfigureDeepgramTextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    case 'openai':
      return (
        <ConfigureOpenAITextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    case 'azure-speech-service':
      return (
        <ConfigureAzureTextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    case 'cartesia':
      return (
        <ConfigureCartesiaTextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    case 'sarvamai':
      return (
        <ConfigureSarvamTextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    case 'rime':
      return (
        <ConfigureRimeTextToSpeech
          parameters={parameters}
          onParameterChange={onChangeParameter}
        />
      );
    default:
      return null;
  }
};
