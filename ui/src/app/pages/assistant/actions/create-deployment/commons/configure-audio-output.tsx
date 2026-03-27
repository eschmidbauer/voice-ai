import { Metadata } from '@rapidaai/react';
import { Dropdown } from '@/app/components/dropdown';
import { FormLabel } from '@/app/components/form-label';
import { FieldSet } from '@/app/components/form/fieldset';
import { Input } from '@/app/components/form/input';
import { InputHelper } from '@/app/components/input-helper';
import { TextToSpeechProvider } from '@/app/components/providers/text-to-speech';
import { useCallback, useState } from 'react';
import {
  GetDefaultSpeakerConfig,
  GetDefaultTextToSpeechIfInvalid,
} from '@/app/components/providers/text-to-speech/provider';
import {
  CONJUNCTION_BOUNDARIES,
  PRONUNCIATION_DICTIONARIES,
} from '@/providers';
import { ChevronDown } from 'lucide-react';
import { cn } from '@/utils';
import { SectionDivider } from '@/app/components/blocks/section-divider';

interface ConfigureAudioOutputProviderProps {
  audioOutputConfig: { provider: string; parameters: Metadata[] };
  setAudioOutputConfig: (config: {
    provider: string;
    parameters: Metadata[];
  }) => void;
}

export const ConfigureAudioOutputProvider: React.FC<
  ConfigureAudioOutputProviderProps
> = ({ audioOutputConfig, setAudioOutputConfig }) => {
  const [showAdvanced, setShowAdvanced] = useState(false);

  const onChangeAudioOutputProvider = (providerName: string) => {
    const parametersToKeep = audioOutputConfig.parameters.filter(p =>
      [
        'speaker.conjunction.boundaries',
        'speaker.conjunction.break',
        'speaker.pronunciation.dictionaries',
      ].includes(p.getKey()),
    );
    setAudioOutputConfig({
      provider: providerName,
      parameters: GetDefaultTextToSpeechIfInvalid(
        providerName,
        GetDefaultSpeakerConfig(parametersToKeep),
      ),
    });
  };

  const onChangeAudioOutputParameter = (parameters: Metadata[]) => {
    if (audioOutputConfig)
      setAudioOutputConfig({ ...audioOutputConfig, parameters });
  };

  const getParamValue = useCallback(
    (key: string, defaultValue: any) => {
      const param = audioOutputConfig.parameters?.find(p => p.getKey() === key);
      return param ? param.getValue() : defaultValue;
    },
    [audioOutputConfig.parameters],
  );

  const updateParameter = (key: string, value: string) => {
    const updatedParams = (audioOutputConfig.parameters || []).map(param => {
      if (param.getKey() === key) {
        const updatedParam = new Metadata();
        updatedParam.setKey(key);
        updatedParam.setValue(value);
        return updatedParam;
      }
      return param;
    });
    if (!updatedParams.some(param => param.getKey() === key)) {
      const newParam = new Metadata();
      newParam.setKey(key);
      newParam.setValue(value);
      updatedParams.push(newParam);
    }
    onChangeAudioOutputParameter(updatedParams);
  };

  return (
    <div className="border-b border-gray-200 dark:border-gray-800">
      <div className="flex flex-col gap-6 max-w-4xl px-6 py-8">
        <TextToSpeechProvider
          onChangeProvider={onChangeAudioOutputProvider}
          onChangeParameter={onChangeAudioOutputParameter}
          provider={audioOutputConfig.provider}
          parameters={audioOutputConfig.parameters}
        />

        {audioOutputConfig.provider && (
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
                <SectionDivider label="Pronunciation" />
                <FieldSet>
                  <FormLabel>Pronunciation Dictionaries</FormLabel>
                  <Dropdown
                    multiple
                    currentValue={getParamValue(
                      'speaker.pronunciation.dictionaries',
                      '',
                    ).split('<|||>')}
                    setValue={v =>
                      updateParameter(
                        'speaker.pronunciation.dictionaries',
                        v.join('<|||>'),
                      )
                    }
                    allValue={PRONUNCIATION_DICTIONARIES}
                    placeholder="Select all that applies"
                    option={c => (
                      <span className="inline-flex items-center gap-2 sm:gap-2.5 max-w-full text-sm font-medium">
                        <span className="truncate capitalize">{c}</span>
                      </span>
                    )}
                    label={c => (
                      <span className="inline-flex items-center gap-2 sm:gap-2.5 max-w-full text-sm font-medium">
                        {c.map(x => (
                          <span key={x} className="truncate">
                            {x}
                          </span>
                        ))}
                      </span>
                    )}
                  />
                  <InputHelper>
                    Pronunciation dictionaries help define custom pronunciations
                    for words, abbreviations, and acronyms. They ensure correct
                    pronunciation of domain-specific terms, names, or technical
                    jargon.
                  </InputHelper>
                </FieldSet>

                <SectionDivider label="Conjunction Boundaries" />
                <FieldSet>
                  <FormLabel>Conjunction Boundaries</FormLabel>
                  <Dropdown
                    multiple
                    currentValue={getParamValue(
                      'speaker.conjunction.boundaries',
                      '',
                    ).split('<|||>')}
                    setValue={v =>
                      updateParameter(
                        'speaker.conjunction.boundaries',
                        v.join('<|||>'),
                      )
                    }
                    allValue={CONJUNCTION_BOUNDARIES}
                    placeholder="Select all that applies"
                    option={c => (
                      <span className="inline-flex items-center gap-2 sm:gap-2.5 max-w-full text-sm font-medium">
                        <span className="truncate capitalize">{c}</span>
                      </span>
                    )}
                    label={c => (
                      <span className="inline-flex items-center gap-2 sm:gap-2.5 max-w-full text-sm font-medium">
                        {c.map(x => (
                          <span key={x} className="truncate">
                            {x}
                          </span>
                        ))}
                      </span>
                    )}
                  />
                  <InputHelper>
                    Conjunctions treated as valid boundaries for adding a pause
                    before delivering to the voice provider.
                  </InputHelper>
                </FieldSet>

                <SectionDivider label="Pause" />
                <FieldSet>
                  <FormLabel>Pause Duration (Milliseconds)</FormLabel>
                  <Input
                    min={100}
                    max={300}
                    className="w-24"
                    value={getParamValue('speaker.conjunction.break', '240')}
                    onChange={e =>
                      updateParameter(
                        'speaker.conjunction.break',
                        e.target.value,
                      )
                    }
                  />
                </FieldSet>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
};
