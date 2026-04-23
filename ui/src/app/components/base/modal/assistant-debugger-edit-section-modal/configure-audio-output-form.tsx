import { Metadata } from '@rapidaai/react';
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
import { ChevronDown } from '@carbon/icons-react';
import { cn } from '@/utils';
import { Stack, TextInput } from '@/app/components/carbon/form';
import { MultiSelect } from '@carbon/react';

interface ConfigureAudioOutputModalFormProps {
  audioOutputConfig: { provider: string; parameters: Metadata[] };
  setAudioOutputConfig: (config: {
    provider: string;
    parameters: Metadata[];
  }) => void;
}

export const ConfigureAudioOutputModalForm: React.FC<
  ConfigureAudioOutputModalFormProps
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

  const pronunciationItems = PRONUNCIATION_DICTIONARIES.map(d => ({
    id: d,
    label: d,
  }));
  const conjunctionItems = CONJUNCTION_BOUNDARIES.map(b => ({
    id: b,
    label: b,
  }));

  const selectedPronunciations = getParamValue(
    'speaker.pronunciation.dictionaries',
    '',
  )
    .split('<|||>')
    .filter(Boolean);
  const selectedConjunctions = getParamValue(
    'speaker.conjunction.boundaries',
    '',
  )
    .split('<|||>')
    .filter(Boolean);

  return (
    <Stack gap={6}>
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
              size={16}
              className={cn(
                'transition-transform duration-200',
                showAdvanced && 'rotate-180',
              )}
            />
            {showAdvanced ? 'Hide' : 'Show'} advanced settings
          </button>

          {showAdvanced && (
            <div className="space-y-6 pt-6 border-t border-gray-200 dark:border-gray-800">
              <p className="text-[10px] font-semibold tracking-[0.12em] uppercase text-gray-500 dark:text-gray-400">
                Pronunciation
              </p>
              <MultiSelect
                id="pronunciation-dictionaries"
                titleText="Pronunciation Dictionaries"
                label="Select all that applies"
                items={pronunciationItems}
                selectedItems={pronunciationItems.filter(i =>
                  selectedPronunciations.includes(i.id),
                )}
                itemToString={(item: any) => item?.label || ''}
                onChange={({ selectedItems }: any) =>
                  updateParameter(
                    'speaker.pronunciation.dictionaries',
                    selectedItems.map((i: any) => i.id).join('<|||>'),
                  )
                }
                helperText="Pronunciation dictionaries help define custom pronunciations for words, abbreviations, and acronyms."
              />

              <p className="text-[10px] font-semibold tracking-[0.12em] uppercase text-gray-500 dark:text-gray-400">
                Conjunction Boundaries
              </p>
              <MultiSelect
                id="conjunction-boundaries"
                titleText="Conjunction Boundaries"
                label="Select all that applies"
                items={conjunctionItems}
                selectedItems={conjunctionItems.filter(i =>
                  selectedConjunctions.includes(i.id),
                )}
                itemToString={(item: any) => item?.label || ''}
                onChange={({ selectedItems }: any) =>
                  updateParameter(
                    'speaker.conjunction.boundaries',
                    selectedItems.map((i: any) => i.id).join('<|||>'),
                  )
                }
                helperText="Conjunctions treated as valid boundaries for adding a pause before delivering to the voice provider."
              />

              <p className="text-[10px] font-semibold tracking-[0.12em] uppercase text-gray-500 dark:text-gray-400">
                Pause
              </p>
              <TextInput
                id="conjunction-break"
                labelText="Pause Duration (Milliseconds)"
                type="number"
                value={getParamValue('speaker.conjunction.break', '240')}
                onChange={e =>
                  updateParameter('speaker.conjunction.break', e.target.value)
                }
              />
            </div>
          )}
        </>
      )}
    </Stack>
  );
};
