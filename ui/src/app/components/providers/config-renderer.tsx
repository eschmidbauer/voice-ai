import React, { useMemo, useRef, useState } from 'react';
import { Metadata } from '@rapidaai/react';
import { SettingsAdjust, Add, TrashCan } from '@carbon/icons-react';
import {
  PrimaryButton,
  SecondaryButton,
} from '@/app/components/carbon/button';
import { cn } from '@/utils';
import { TextInput, TextArea } from '@/app/components/carbon/form';
import { TertiaryButton } from '@/app/components/carbon/button';
import {
  Select as CarbonSelect,
  SelectItem,
  NumberInput,
  Slider,
  Button,
  Dropdown as CarbonDropdown,
  ComboBox,
  ButtonSet,
  ComposedModal,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from '@carbon/react';
import {
  CategoryConfig,
  ParameterConfig,
  ProviderConfig,
  isModelSelectorParameter,
  loadProviderData,
  resolveCategoryParameters,
} from '@/providers/config-loader';
import { getDefaultsFromConfig } from '@/providers/config-defaults';

export const ConfigRenderer: React.FC<{
  provider: string;
  category: 'stt' | 'tts' | 'text' | 'vad' | 'eos' | 'noise' | 'telemetry';
  config: CategoryConfig;
  parameters: Metadata[] | null;
  onParameterChange: (parameters: Metadata[]) => void;
}> = ({ provider, category, config, parameters, onParameterChange }) => {
  const effectiveParameters = useMemo(
    () =>
      resolveCategoryParameters(provider, category, config, parameters || []),
    [provider, category, config, parameters],
  );

  const getParamValue = (key: string) =>
    parameters?.find(p => p.getKey() === key)?.getValue() ?? '';

  const isModelSelector = (param: ParameterConfig): boolean =>
    isModelSelectorParameter(param) &&
    (category === 'stt' || category === 'tts' || category === 'text');

  const applyUpdates = (
    updates: { key: string; value: string }[],
    sourceParam?: ParameterConfig,
  ) => {
    const updatedParams = [...(parameters || [])];
    const currentModelValue =
      sourceParam && isModelSelector(sourceParam)
        ? getParamValue(sourceParam.key)
        : '';

    for (const { key, value } of updates) {
      const existingIndex = updatedParams.findIndex(p => p.getKey() === key);
      const newParam = new Metadata();
      newParam.setKey(key);
      newParam.setValue(value);
      if (existingIndex >= 0) {
        updatedParams[existingIndex] = newParam;
      } else {
        updatedParams.push(newParam);
      }
    }

    if (!sourceParam || !isModelSelector(sourceParam)) {
      onParameterChange(updatedParams);
      return;
    }
    const nextModelValue = updates.find(
      update => update.key === sourceParam.key,
    )?.value;
    if (nextModelValue === undefined || nextModelValue === currentModelValue) {
      onParameterChange(updatedParams);
      return;
    }

    const includeCredential = updatedParams.some(
      p => p.getKey() === 'rapida.credential_id',
    );
    const wrappedConfig = { [category]: config } as ProviderConfig;
    const hydrated = getDefaultsFromConfig(
      wrappedConfig,
      category,
      updatedParams,
      provider,
      { includeCredential },
    );
    onParameterChange(hydrated);
  };

  const updateParameter = (
    key: string,
    value: string,
    sourceParam?: ParameterConfig,
  ) => {
    applyUpdates([{ key, value }], sourceParam);
  };

  const updateMultipleParameters = (
    updates: { key: string; value: string }[],
    sourceParam?: ParameterConfig,
  ) => {
    applyUpdates(updates, sourceParam);
  };

  const isVisible = (param: ParameterConfig): boolean => {
    if (!param.showWhen) return true;
    const refValue = getParamValue(param.showWhen.key);
    return new RegExp(param.showWhen.pattern).test(refValue);
  };

  const visibleParameters = effectiveParameters.filter(isVisible);
  const regularParams = visibleParameters.filter(p => !p.advanced);
  const advancedParams = visibleParameters.filter(p => p.advanced);
  const hasAdvanced = advancedParams.length > 0;

  const renderField = (param: ParameterConfig) => {
    if (!isVisible(param)) return null;

    const colSpanClass = param.colSpan === 2 ? 'col-span-2' : 'col-span-1';

    switch (param.type) {
      case 'dropdown':
        return (
          <DropdownField
            key={param.key}
            param={param}
            provider={provider}
            value={getParamValue(param.key)}
            onChange={(value, selectedItem) => {
              if (param.linkedField && selectedItem) {
                updateMultipleParameters(
                  [
                    { key: param.key, value },
                    {
                      key: param.linkedField.key,
                      value: selectedItem[param.linkedField.sourceField] ?? '',
                    },
                  ],
                  param,
                );
              } else {
                updateParameter(param.key, value, param);
              }
            }}
            colSpanClass={colSpanClass}
          />
        );

      case 'slider':
        const sliderRawValue = getParamValue(param.key);
        const sliderParsedValue = Number.parseFloat(sliderRawValue);
        const sliderValue = Number.isNaN(sliderParsedValue)
          ? (param.min ?? 0)
          : sliderParsedValue;
        return (
          <div className={cn(colSpanClass)} key={param.key}>
            <Slider
              id={`slider-${param.key}`}
              labelText={param.label}
              min={param.min ?? 0}
              max={param.max ?? 1}
              step={param.step ?? 0.1}
              value={sliderValue}
              onChange={({ value: v }: { value: number }) => updateParameter(param.key, v.toString())}
            />
            {param.helpText && <p className="text-xs text-gray-500 mt-1">{param.helpText}</p>}
          </div>
        );

      case 'number':
        return (
          <div className={cn(colSpanClass)} key={param.key}>
            <TextInput
              id={`num-${param.key}`}
              labelText={param.label}
              type="number"
              min={param.min}
              max={param.max}
              step={param.step}
              value={getParamValue(param.key)}
              placeholder={param.placeholder}
              helperText={param.helpText}
              onChange={e => updateParameter(param.key, e.target.value)}
            />
          </div>
        );

      case 'input':
        return (
          <div className={cn(colSpanClass)} key={param.key}>
            <TextInput
              id={`input-${param.key}`}
              labelText={param.label}
              value={getParamValue(param.key)}
              placeholder={param.placeholder}
              helperText={param.helpText}
              onChange={e => updateParameter(param.key, e.target.value)}
            />
          </div>
        );

      case 'textarea':
        return (
          <div className={cn(colSpanClass)} key={param.key}>
            <TextArea
              id={`textarea-${param.key}`}
              labelText={param.label}
              required={param.required !== false}
              value={getParamValue(param.key)}
              rows={param.rows ?? 2}
              placeholder={param.placeholder}
              helperText={param.helpText}
              onChange={e => updateParameter(param.key, e.target.value)}
            />
          </div>
        );

      case 'select':
        return (
          <div className={cn(colSpanClass)} key={param.key}>
            <CarbonSelect
              id={`select-${param.key}`}
              labelText={param.label}
              value={getParamValue(param.key)}
              helperText={param.helpText}
              onChange={e => updateParameter(param.key, e.target.value)}
            >
              <SelectItem value="" text={`Select ${param.label.toLowerCase()}`} />
              {(param.choices ?? []).map(c => (
                <SelectItem key={c.value} value={c.value} text={c.label} />
              ))}
            </CarbonSelect>
          </div>
        );

      case 'json':
        return (
          <div className={cn(colSpanClass)} key={param.key}>
            <TextArea
              id={`json-${param.key}`}
              labelText={param.label}
              placeholder="Enter as JSON"
              value={getParamValue(param.key) || '{}'}
              helperText={param.helpText}
              onChange={e => updateParameter(param.key, e.target.value)}
            />
          </div>
        );

      case 'key_value':
        return (
          <KeyValueField
            key={param.key}
            param={param}
            value={getParamValue(param.key)}
            onChange={value => updateParameter(param.key, value)}
            colSpanClass={colSpanClass}
          />
        );

      default:
        return null;
    }
  };

  if (category === 'text' && hasAdvanced) {
    const mainParam = regularParams[0];
    return (
      <TextCategoryLayout
        mainParam={mainParam}
        provider={provider}
        advancedParams={advancedParams}
        getParamValue={getParamValue}
        updateMultipleParameters={updateMultipleParameters}
        updateParameter={updateParameter}
        renderField={renderField}
        parameters={parameters}
        onParameterChange={onParameterChange}
      />
    );
  }

  return <>{effectiveParameters.map(renderField)}</>;
};

const TextCategoryLayout: React.FC<{
  mainParam?: ParameterConfig;
  provider: string;
  advancedParams: ParameterConfig[];
  getParamValue: (key: string) => string;
  updateMultipleParameters: (
    updates: { key: string; value: string }[],
    sourceParam?: ParameterConfig,
  ) => void;
  updateParameter: (
    key: string,
    value: string,
    sourceParam?: ParameterConfig,
  ) => void;
  renderField: (param: ParameterConfig) => React.ReactNode;
  parameters: Metadata[] | null;
  onParameterChange: (parameters: Metadata[]) => void;
}> = ({
  mainParam,
  provider,
  advancedParams,
  getParamValue,
  updateMultipleParameters,
  updateParameter,
  renderField,
  parameters,
  onParameterChange,
}) => {
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const snapshotRef = useRef<Metadata[] | null>(null);

  const handleOpen = () => {
    snapshotRef.current = parameters ? [...parameters] : null;
    setAdvancedOpen(true);
  };

  const handleClose = () => {
    if (snapshotRef.current) {
      onParameterChange(snapshotRef.current);
    }
    snapshotRef.current = null;
    setAdvancedOpen(false);
  };

  const handleComplete = () => {
    snapshotRef.current = null;
    setAdvancedOpen(false);
  };

  return (
    <div className="flex-1 flex items-stretch">
      <div className="flex-1 min-w-0">
        {mainParam?.type === 'dropdown' &&
          renderTextMainDropdown(
            mainParam,
            provider,
            getParamValue,
            updateMultipleParameters,
            updateParameter,
          )}
      </div>
      <div className="shrink-0 border-l border-gray-200 dark:border-gray-700">
        <Button
          hasIconOnly
          renderIcon={SettingsAdjust}
          iconDescription="Advanced settings"
          kind="ghost"
          size="md"
          onClick={handleOpen}
        />
        <ComposedModal
          open={advancedOpen}
          onClose={handleClose}
          preventCloseOnClickOutside
          size="lg"
        >
          <ModalHeader title="Advanced Settings" />
          <ModalBody>
            <div className="grid grid-cols-3 gap-4">
              {advancedParams.map(renderField)}
            </div>
          </ModalBody>
          <ModalFooter>
            <SecondaryButton onClick={handleClose}>
              Close
            </SecondaryButton>
            <PrimaryButton onClick={handleComplete}>
              Complete
            </PrimaryButton>
          </ModalFooter>
        </ComposedModal>
      </div>
    </div>
  );
};

const DropdownField: React.FC<{
  param: ParameterConfig;
  provider: string;
  value: string;
  onChange: (value: string, selectedItem?: any) => void;
  colSpanClass: string;
}> = ({ param, provider, value, onChange, colSpanClass }) => {
  const data = param.data ? loadProviderData(provider, param.data) : [];
  const valueField = param.valueField || 'id';
  const selectedItem = data.find((item: any) => item[valueField] === value) || null;

  if (param.customValue || param.searchable) {
    return (
      <div className={cn(colSpanClass)} key={param.key}>
        <ComboBox
          id={`combo-${param.key}`}
          titleText={param.label}
          items={data}
          selectedItem={selectedItem}
          itemToString={(item: any) => item?.name || ''}
          placeholder={`Select ${param.label.toLowerCase()}`}
          onChange={({ selectedItem: item }: any) => {
            if (item) {
              onChange(item[valueField], item);
            }
          }}
          onInputChange={(inputValue: string) => {
            if (param.customValue && inputValue && !data.find((d: any) => d.name === inputValue)) {
              onChange(inputValue);
            }
          }}
          allowCustomValue={param.customValue}
          helperText={param.helpText}
        />
      </div>
    );
  }

  return (
    <div className={cn(colSpanClass)} key={param.key}>
      <CarbonDropdown
        id={`dropdown-${param.key}`}
        titleText={param.label}
        label={`Select ${param.label.toLowerCase()}`}
        items={data}
        selectedItem={selectedItem}
        itemToString={(item: any) => item?.name || ''}
        onChange={({ selectedItem: item }: any) => {
          if (item) {
            onChange(item[valueField], item);
          }
        }}
        helperText={param.helpText}
      />
    </div>
  );
};

const KeyValueField: React.FC<{
  param: ParameterConfig;
  value: string;
  onChange: (value: string) => void;
  colSpanClass: string;
}> = ({ param, value, onChange, colSpanClass }) => {
  const parseEntries = (raw: string): { key: string; value: string }[] => {
    if (!raw) return [];
    return raw
      .split(',')
      .map(pair => {
        const idx = pair.indexOf('=');
        if (idx < 0) return { key: pair.trim(), value: '' };
        return { key: pair.slice(0, idx).trim(), value: pair.slice(idx + 1).trim() };
      })
      .filter(e => e.key || e.value);
  };

  const serialize = (entries: { key: string; value: string }[]): string =>
    entries.map(e => `${e.key}=${e.value}`).join(',');

  const entries = parseEntries(value);

  const updateEntry = (index: number, field: 'key' | 'value', val: string) => {
    const next = [...entries];
    next[index] = { ...next[index], [field]: val };
    onChange(serialize(next));
  };

  const removeEntry = (index: number) => {
    onChange(serialize(entries.filter((_, i) => i !== index)));
  };

  const addEntry = () => {
    onChange(serialize([...entries, { key: '', value: '' }]));
  };

  return (
    <div className={cn(colSpanClass)} key={param.key}>
      <p className="text-xs font-medium mb-2">{param.label} ({entries.length})</p>
      <div className="border border-gray-200 dark:border-gray-700 divide-y divide-gray-200 dark:divide-gray-700">
        {entries.map((entry, index) => (
          <div key={index} className="flex items-center gap-2 px-2 py-1.5">
            <TextInput
              id={`kv-key-${param.key}-${index}`}
              labelText=""
              hideLabel
              value={entry.key}
              onChange={e => updateEntry(index, 'key', e.target.value)}
              placeholder="Key"
              size="md"
              className="flex-1"
            />
            <span className="text-xs text-gray-400 shrink-0">=</span>
            <TextInput
              id={`kv-val-${param.key}-${index}`}
              labelText=""
              hideLabel
              value={entry.value}
              onChange={e => updateEntry(index, 'value', e.target.value)}
              placeholder="Value"
              size="md"
              className="flex-1"
            />
            <Button
              hasIconOnly
              renderIcon={TrashCan}
              iconDescription="Remove"
              kind="danger--ghost"
              size="md"
              onClick={() => removeEntry(index)}
            />
          </div>
        ))}
      </div>
      <div className="pt-4">
        <TertiaryButton
          size="md"
          renderIcon={Add}
          onClick={addEntry}
          className="!w-full !max-w-none"
        >
          Add {param.label.toLowerCase()}
        </TertiaryButton>
      </div>
      {param.helpText && <p className="text-xs text-gray-500 mt-1">{param.helpText}</p>}
    </div>
  );
};

function renderTextMainDropdown(
  param: ParameterConfig,
  provider: string,
  getParamValue: (key: string) => string,
  updateMultipleParameters: (
    updates: { key: string; value: string }[],
    sourceParam?: ParameterConfig,
  ) => void,
  updateParameter: (
    key: string,
    value: string,
    sourceParam?: ParameterConfig,
  ) => void,
) {
  const data = param.data ? loadProviderData(provider, param.data) : [];
  const valueField = param.valueField || 'id';
  const currentValue = getParamValue(param.key);
  const selectedItem = data.find((x: any) => x[valueField] === currentValue) || null;

  const handleSelect = (item: any) => {
    if (!item) return;
    if (param.linkedField) {
      updateMultipleParameters(
        [
          { key: param.key, value: item[valueField] },
          {
            key: param.linkedField.key,
            value: item[param.linkedField.sourceField] ?? item[valueField],
          },
        ],
        param,
      );
    } else {
      updateParameter(param.key, item[valueField], param);
    }
  };

  const handleCustom = (vl: string) => {
    if (param.linkedField) {
      updateMultipleParameters(
        [
          { key: param.key, value: vl },
          { key: param.linkedField.key, value: vl },
        ],
        param,
      );
    } else {
      updateParameter(param.key, vl, param);
    }
  };

  if (param.customValue) {
    return (
      <ComboBox
        id={`text-main-combo-${param.key}`}
        titleText=""
        hideLabel
        items={data}
        size="md"
        selectedItem={selectedItem}
        itemToString={(item: any) => item?.name || ''}
        placeholder="Select model"
        onChange={({ selectedItem: item }: any) => {
          if (item) handleSelect(item);
        }}
        onInputChange={(inputValue: string) => {
          if (inputValue && !data.find((d: any) => d.name === inputValue)) {
            handleCustom(inputValue);
          }
        }}
        allowCustomValue
      />
    );
  }

  return (
    <CarbonDropdown
      id={`text-main-dropdown-${param.key}`}
      titleText=""
      hideLabel
      label="Select model"
      size="md"
      items={data}
      selectedItem={selectedItem}
      itemToString={(item: any) => item?.name || ''}
      onChange={({ selectedItem: item }: any) => {
        if (item) handleSelect(item);
      }}
    />
  );
}
