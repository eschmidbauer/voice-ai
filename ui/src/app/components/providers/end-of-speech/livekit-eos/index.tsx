import { Metadata } from '@rapidaai/react';
import { Dropdown } from '@/app/components/dropdown';
import { FormLabel } from '@/app/components/form-label';
import { FieldSet } from '@/app/components/form/fieldset';
import { SliderField } from '@/app/components/providers/end-of-speech/slider-field';
import { useEosParams } from '@/app/components/providers/end-of-speech/use-eos-params';
import { BlueNoticeBlock } from '../../../container/message/notice-block/index';

const MODEL_OPTIONS = [
  { code: 'en', name: 'English (66MB, optimized)' },
  { code: 'multilingual', name: 'Multilingual (378MB, 14 languages)' },
];

const renderLabel = (c: { name: string }) => (
  <span className="inline-flex items-center gap-2 text-sm font-medium">
    <span className="truncate">{c.name}</span>
  </span>
);

export const ConfigureLivekitEOS: React.FC<{
  onParameterChange: (parameters: Metadata[]) => void;
  parameters: Metadata[];
}> = ({ onParameterChange, parameters }) => {
  const { get, set } = useEosParams(parameters, onParameterChange);
  const currentModel = get('microphone.eos.model', 'en');

  return (
    <>
      <BlueNoticeBlock className="text-xs">
        Uses a language model to predict turn completion from transcribed text.
        Reduces false triggers on natural pauses like addresses and numbers.
        {currentModel === 'multilingual' &&
          ' Multilingual model supports: zh, de, nl, en, pt, es, fr, it, ja, ko, ru, tr, id, hi.'}
      </BlueNoticeBlock>

      <FieldSet>
        <FormLabel>Model</FormLabel>
        <Dropdown
          className="bg-light-background max-w-full dark:bg-gray-950"
          currentValue={MODEL_OPTIONS.find(x => x.code === currentModel)}
          setValue={v => set('microphone.eos.model', v.code)}
          allValue={MODEL_OPTIONS}
          placeholder="Select model"
          option={renderLabel}
          label={renderLabel}
        />
      </FieldSet>

      <SliderField
        label="Turn Completion Threshold"
        hint="Probability threshold above which the model considers the turn complete. Lower = faster response, higher = fewer interruptions."
        min={0.001}
        max={0.1}
        step={0.001}
        inputWidth="w-20"
        parse={parseFloat}
        value={get('microphone.eos.threshold', '0.0289')}
        onChange={v => set('microphone.eos.threshold', v)}
      />
      <SliderField
        label="Quick Timeout"
        hint="Short buffer after model says 'done' before firing EOS (ms). Catches fast corrections like 'yes... actually wait'."
        min={50}
        max={500}
        step={50}
        value={get('microphone.eos.quick_timeout', '250')}
        onChange={v => set('microphone.eos.quick_timeout', v)}
      />
      <SliderField
        label="Safety Timeout"
        hint="Maximum silence before forcing EOS when model keeps saying 'not done' (ms). Acts as a safety fallback."
        min={500}
        max={5000}
        step={100}
        value={get('microphone.eos.extended_timeout', '1500')}
        onChange={v => set('microphone.eos.extended_timeout', v)}
      />
      <SliderField
        label="Fallback Timeout"
        hint="Silence timeout used for interim transcripts and when model inference fails (ms)."
        min={300}
        max={2000}
        step={100}
        value={get('microphone.eos.fallback_timeout', '500')}
        onChange={v => set('microphone.eos.fallback_timeout', v)}
      />
    </>
  );
};
