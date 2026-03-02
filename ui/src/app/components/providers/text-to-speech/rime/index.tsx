/**
 * Rapida – Open Source Voice AI Orchestration Platform
 * Copyright (C) 2023-2025 Prashant Srivastav <prashant@rapida.ai>
 * Licensed under a modified GPL-2.0. See the LICENSE file for details.
 */
import { Metadata } from '@rapidaai/react';
import { FormLabel } from '@/app/components/form-label';
import { FieldSet } from '@/app/components/form/fieldset';
import { useState } from 'react';
import { RIME_LANGUAGE, RIME_MODEL, RIME_VOICE } from '@/providers';
import { CustomValueDropdown } from '@/app/components/dropdown/custom-value-dropdown';
import { Dropdown } from '@/app/components/dropdown';
export { GetRimeDefaultOptions, ValidateRimeOptions } from './constant';

const renderOption = (c: { name: string }) => {
  return (
    <span className="inline-flex items-center gap-2 sm:gap-2.5 max-w-full text-sm font-medium">
      <span className="truncate capitalize">{c.name}</span>
    </span>
  );
};

export const ConfigureRimeTextToSpeech: React.FC<{
  onParameterChange: (parameters: Metadata[]) => void;
  parameters: Metadata[] | null;
}> = ({ onParameterChange, parameters }) => {
  const [filteredVoices, setFilteredVoices] = useState(RIME_VOICE());

  const getParamValue = (key: string) =>
    parameters?.find(p => p.getKey() === key)?.getValue() ?? '';

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
    onParameterChange(updatedParams);
  };

  return (
    <>
      <FieldSet className="col-span-1" key="speak.model">
        <FormLabel>Model</FormLabel>
        <Dropdown
          className="bg-light-background max-w-full dark:bg-gray-950"
          currentValue={RIME_MODEL().find(
            x => x.model_id === getParamValue('speak.model'),
          )}
          setValue={(v: { model_id: string }) =>
            updateParameter('speak.model', v.model_id)
          }
          allValue={RIME_MODEL()}
          placeholder="Select model"
          option={renderOption}
          label={renderOption}
        />
      </FieldSet>
      <FieldSet className="col-span-1" key="speak.voice.id">
        <FormLabel>Voice</FormLabel>
        <CustomValueDropdown
          searchable
          className="bg-light-background max-w-full dark:bg-gray-950"
          currentValue={filteredVoices.find(
            x => x.voice_id === getParamValue('speak.voice.id'),
          )}
          setValue={(v: { voice_id: string }) =>
            updateParameter('speak.voice.id', v.voice_id)
          }
          allValue={filteredVoices}
          customValue
          onSearching={t => {
            const voices = RIME_VOICE();
            const v = t.target.value;
            if (v.length > 0) {
              setFilteredVoices(
                voices.filter(
                  voice =>
                    voice.name.toLowerCase().includes(v.toLowerCase()) ||
                    voice.voice_id.toLowerCase().includes(v.toLowerCase()) ||
                    voice.category.toLowerCase().includes(v.toLowerCase()),
                ),
              );
              return;
            }
            setFilteredVoices(voices);
          }}
          onAddCustomValue={vl => {
            filteredVoices.push({ voice_id: vl, name: vl });
            updateParameter('speak.voice.id', vl);
          }}
          placeholder="Select voice"
          option={renderOption}
          label={renderOption}
        />
      </FieldSet>
      <FieldSet className="col-span-1" key="speak.language">
        <FormLabel>Language</FormLabel>
        <Dropdown
          className="bg-light-background max-w-full dark:bg-gray-950"
          currentValue={RIME_LANGUAGE().find(
            x => x.language_id === getParamValue('speak.language'),
          )}
          setValue={(v: { language_id: string }) =>
            updateParameter('speak.language', v.language_id)
          }
          allValue={RIME_LANGUAGE()}
          placeholder="Select language"
          option={renderOption}
          label={renderOption}
        />
      </FieldSet>
      <FieldSet className="col-span-1" key="speak.speed_alpha">
        <FormLabel>Speed Alpha</FormLabel>
        <input
          type="number"
          step="0.1"
          min="0.1"
          max="3.0"
          className="w-full rounded border border-gray-200 bg-light-background px-3 py-2 text-sm dark:border-gray-800 dark:bg-gray-950"
          placeholder="1.0 (default)"
          value={getParamValue('speak.speed_alpha')}
          onChange={e => updateParameter('speak.speed_alpha', e.target.value)}
        />
      </FieldSet>
    </>
  );
};
