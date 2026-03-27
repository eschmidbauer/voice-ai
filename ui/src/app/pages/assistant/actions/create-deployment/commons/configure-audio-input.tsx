import { useCallback, useState } from 'react';
import { SpeechToTextProvider } from '@/app/components/providers/speech-to-text';
import { NoiseCancellationProvider } from '@/app/components/providers/noise-removal';
import { GetDefaultNoiseCancellationConfig } from '@/app/components/providers/noise-removal/provider';
import { EndOfSpeechProvider } from '@/app/components/providers/end-of-speech';
import { Metadata } from '@rapidaai/react';
import {
  GetDefaultMicrophoneConfig,
  GetDefaultSpeechToTextIfInvalid,
} from '@/app/components/providers/speech-to-text/provider';
import { GetDefaultEOSConfig } from '@/app/components/providers/end-of-speech/provider';
import { GetDefaultVADConfig } from '@/app/components/providers/vad/provider';
import { VADProvider } from '@/app/components/providers/vad';
import { ChevronDown } from 'lucide-react';
import { cn } from '@/utils';
import { SectionDivider } from '@/app/components/blocks/section-divider';

interface ConfigureAudioInputProviderProps {
  audioInputConfig: { provider: string; parameters: Metadata[] };
  setAudioInputConfig: (config: {
    provider: string;
    parameters: Metadata[];
  }) => void;
}

export const ConfigureAudioInputProvider: React.FC<
  ConfigureAudioInputProviderProps
> = ({ audioInputConfig, setAudioInputConfig }) => {
  const [showAdvanced, setShowAdvanced] = useState(false);

  const keepAdvancedMicrophoneParams = (parameters: Metadata[]) =>
    parameters.filter(p => {
      const key = p.getKey();
      return (
        key.startsWith('microphone.eos.') ||
        key.startsWith('microphone.vad.') ||
        key.startsWith('microphone.denoising.')
      );
    });

  const onChangeAudioInputProvider = (providerName: string) => {
    const microphoneScopedParameters = keepAdvancedMicrophoneParams(
      audioInputConfig.parameters,
    );
    setAudioInputConfig({
      provider: providerName,
      parameters: GetDefaultSpeechToTextIfInvalid(
        providerName,
        GetDefaultMicrophoneConfig(microphoneScopedParameters),
      ),
    });
  };

  const onChangeAudioInputParameter = (parameters: Metadata[]) => {
    if (audioInputConfig)
      setAudioInputConfig({ ...audioInputConfig, parameters });
  };

  const getParamValue = useCallback(
    (key: string, defaultValue: any) => {
      const param = audioInputConfig.parameters?.find(p => p.getKey() === key);
      return param ? param.getValue() : defaultValue;
    },
    [audioInputConfig.parameters],
  );

  return (
    <div className="border-b border-gray-200 dark:border-gray-800">
      <div className="flex flex-col gap-6 max-w-4xl  px-6 py-8">
        <SpeechToTextProvider
          onChangeProvider={onChangeAudioInputProvider}
          onChangeParameter={onChangeAudioInputParameter}
          provider={audioInputConfig.provider}
          parameters={audioInputConfig.parameters}
        />
        {audioInputConfig.provider && (
          <>
            <button
              type="button"
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="flex items-center gap-1.5 text-xs font-medium text-gray-500 hover:text-gray-800 dark:hover:text-gray-200 transition-colors"
            >
              <ChevronDown
                className={cn(
                  'w-4 h-4 transition-transform duration-200',
                  showAdvanced && 'rotate-180',
                )}
                strokeWidth={2}
              />
              {showAdvanced ? 'Hide' : 'Show'} advanced settings
            </button>

            {showAdvanced && (
              <div className="flex flex-col gap-6 pt-6 border-t border-gray-200 dark:border-gray-800">
                <SectionDivider label="Voice Activity Detection" />
                <VADProvider
                  provider={getParamValue(
                    'microphone.vad.provider',
                    'silero_vad',
                  )}
                  onChangeProvider={v =>
                    onChangeAudioInputParameter(
                      GetDefaultVADConfig(v, audioInputConfig.parameters),
                    )
                  }
                  parameters={audioInputConfig.parameters}
                  onChangeParameter={onChangeAudioInputParameter}
                />
                <SectionDivider label="Background Noise" />
                <NoiseCancellationProvider
                  noiseCancellationProvider={getParamValue(
                    'microphone.denoising.provider',
                    'rn_noise',
                  )}
                  parameters={audioInputConfig.parameters}
                  onChangeParameter={onChangeAudioInputParameter}
                  onChangeNoiseCancellationProvider={v =>
                    onChangeAudioInputParameter(
                      GetDefaultNoiseCancellationConfig(
                        v,
                        audioInputConfig.parameters,
                      ),
                    )
                  }
                />
                <SectionDivider label="End of Speech" />
                <EndOfSpeechProvider
                  provider={getParamValue(
                    'microphone.eos.provider',
                    'pipecat_smart_turn_eos',
                  )}
                  onChangeProvider={provider =>
                    onChangeAudioInputParameter(
                      GetDefaultEOSConfig(
                        provider,
                        audioInputConfig.parameters.filter(
                          p => !p.getKey().startsWith('microphone.eos.'),
                        ),
                      ),
                    )
                  }
                  parameters={audioInputConfig.parameters}
                  onChangeParameter={onChangeAudioInputParameter}
                />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
};
