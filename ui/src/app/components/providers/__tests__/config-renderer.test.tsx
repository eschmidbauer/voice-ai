/**
 * Component tests for ConfigRenderer.
 *
 * Tests that the generic UI renderer correctly renders fields based on
 * category JSON parameter types and calls onParameterChange appropriately.
 */
import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Metadata } from '@rapidaai/react';
import { ConfigRenderer } from '../config-renderer';
import { CategoryConfig } from '@/providers/config-loader';

jest.mock('@/utils', () => ({
  cn: (...inputs: any[]) => inputs.filter(Boolean).join(' '),
}));

jest.mock('@/app/components/dropdown', () => {
  const React = require('react');
  return {
    Dropdown: ({ currentValue, setValue, allValue, placeholder }: any) =>
      React.createElement(
        'div',
        null,
        placeholder ? React.createElement('span', null, placeholder) : null,
        React.createElement(
          'select',
          {
            value:
              currentValue?.id ??
              currentValue?.code ??
              currentValue?.value ??
              currentValue?.model_id ??
              currentValue?.voice_id ??
              currentValue?.language_id ??
              '',
            onChange: (e: any) => {
              const selected = (allValue || []).find(
                (item: any) =>
                  item.id === e.target.value ||
                  item.code === e.target.value ||
                  item.value === e.target.value ||
                  item.model_id === e.target.value ||
                  item.voice_id === e.target.value ||
                  item.language_id === e.target.value,
              );
              if (selected) setValue(selected);
            },
          },
          React.createElement(
            'option',
            { value: '' },
            placeholder || 'Select option',
          ),
          ...(allValue || []).map((item: any) => {
            const value =
              item.id ??
              item.code ??
              item.value ??
              item.model_id ??
              item.voice_id ??
              item.language_id;
            const label = item.name ?? item.label ?? String(value);
            return React.createElement(
              'option',
              { key: String(value), value: String(value) },
              label,
            );
          }),
        ),
      ),
  };
});

jest.mock('@/app/components/dropdown/custom-value-dropdown', () => {
  const React = require('react');
  return {
    CustomValueDropdown: ({ currentValue, setValue, allValue, placeholder }: any) =>
      React.createElement(
        'div',
        null,
        placeholder ? React.createElement('span', null, placeholder) : null,
        React.createElement(
          'select',
          {
            value:
              currentValue?.id ??
              currentValue?.code ??
              currentValue?.value ??
              currentValue?.model_id ??
              currentValue?.voice_id ??
              currentValue?.language_id ??
              '',
            onChange: (e: any) => {
              const selected = (allValue || []).find(
                (item: any) =>
                  item.id === e.target.value ||
                  item.code === e.target.value ||
                  item.value === e.target.value ||
                  item.model_id === e.target.value ||
                  item.voice_id === e.target.value ||
                  item.language_id === e.target.value,
              );
              if (selected) setValue(selected);
            },
          },
          React.createElement(
            'option',
            { value: '' },
            placeholder || 'Select option',
          ),
          ...(allValue || []).map((item: any) => {
            const value =
              item.id ??
              item.code ??
              item.value ??
              item.model_id ??
              item.voice_id ??
              item.language_id;
            const label = item.name ?? item.label ?? String(value);
            return React.createElement(
              'option',
              { key: String(value), value: String(value) },
              label,
            );
          }),
        ),
      ),
  };
});

jest.mock('@/app/components/carbon/form', () => {
  const React = require('react');
  return {
    TextInput: ({
      id,
      labelText,
      value,
      onChange,
      placeholder,
      type = 'text',
      helperText,
    }: any) =>
      React.createElement(
        'div',
        null,
        labelText ? React.createElement('label', { htmlFor: id }, labelText) : null,
        React.createElement('input', {
          id,
          value: value ?? '',
          onChange,
          placeholder,
          type,
        }),
        helperText ? React.createElement('p', null, helperText) : null,
      ),
    TextArea: ({ id, labelText, value, onChange, placeholder, helperText }: any) =>
      React.createElement(
        'div',
        null,
        labelText ? React.createElement('label', { htmlFor: id }, labelText) : null,
        React.createElement('textarea', {
          id,
          value: value ?? '',
          onChange,
          placeholder,
        }),
        helperText ? React.createElement('p', null, helperText) : null,
      ),
  };
});

jest.mock('@carbon/icons-react', () => ({
  Settings: () => null,
  Close: () => null,
  Add: () => null,
  TrashCan: () => null,
}));

jest.mock('@carbon/react', () => {
  const React = require('react');
  const getValue = (item: any) =>
    item?.id ?? item?.code ?? item?.value ?? item?.name ?? '';

  return {
    Dropdown: ({ id, titleText, label, items = [], selectedItem, onChange }: any) =>
      React.createElement(
        'div',
        null,
        titleText ? React.createElement('span', null, titleText) : null,
        React.createElement(
          'select',
          {
            id,
            role: 'combobox',
            value: getValue(selectedItem),
            onChange: (e: any) => {
              const item = items.find((i: any) => String(getValue(i)) === e.target.value);
              onChange?.({ selectedItem: item || null });
            },
          },
          React.createElement('option', { value: '' }, label || 'Select'),
          ...items.map((item: any) =>
            React.createElement(
              'option',
              { key: String(getValue(item)), value: String(getValue(item)) },
              item?.name || String(getValue(item)),
            ),
          ),
        ),
      ),
    ComboBox: ({ id, titleText, items = [], selectedItem, onChange }: any) =>
      React.createElement(
        'div',
        null,
        titleText ? React.createElement('span', null, titleText) : null,
        React.createElement(
          'select',
          {
            id,
            role: 'combobox',
            value: getValue(selectedItem),
            onChange: (e: any) => {
              const item = items.find((i: any) => String(getValue(i)) === e.target.value);
              onChange?.({ selectedItem: item || null });
            },
          },
          React.createElement('option', { value: '' }, 'Select'),
          ...items.map((item: any) =>
            React.createElement(
              'option',
              { key: String(getValue(item)), value: String(getValue(item)) },
              item?.name || String(getValue(item)),
            ),
          ),
        ),
      ),
    Slider: ({ id, value, onChange, labelText }: any) =>
      React.createElement(
        'div',
        null,
        labelText ? React.createElement('label', { htmlFor: id }, labelText) : null,
        React.createElement('input', {
          id,
          type: 'range',
          role: 'slider',
          value: value ?? 0,
          onChange: (e: any) => onChange?.({ value: Number(e.target.value) }),
        }),
      ),
    Select: ({ id, labelText, value, onChange, children }: any) =>
      React.createElement(
        'div',
        null,
        labelText ? React.createElement('label', { htmlFor: id }, labelText) : null,
        React.createElement('select', { id, value, onChange }, children),
      ),
    SelectItem: ({ value, text }: any) =>
      React.createElement('option', { value }, text),
    NumberInput: ({ id, value, onChange }: any) =>
      React.createElement('input', {
        id,
        type: 'number',
        value,
        onChange: (e: any) => onChange?.(e, { value: e.target.value }),
      }),
    Modal: ({ children, open }: any) =>
      open ? React.createElement('div', null, children) : null,
    ComposedModal: ({ children, open }: any) =>
      open ? React.createElement('div', null, children) : null,
    ModalHeader: ({ title }: any) => React.createElement('div', null, title),
    ModalBody: ({ children }: any) => React.createElement('div', null, children),
    ModalFooter: ({ children }: any) => React.createElement('div', null, children),
    Button: ({ children, hasIconOnly: _, renderIcon: _r, iconDescription: _d, ...props }: any) => React.createElement('button', props, children),
  };
});

// Mock the loadProviderData to return controlled test data
jest.mock('@/providers/config-loader', () => {
  const actual = jest.requireActual('@/providers/config-loader');
  const mockedLoadProviderData = (provider: string, filename: string) => {
    if (filename === 'models.json') {
      return [
        {
          id: 'model-a',
          name: 'Model A',
          config: {
            parameters: [
              {
                key: 'model.temperature',
                label: 'Temperature',
                type: 'number',
                required: true,
                default: '0.2',
              },
            ],
          },
        },
        {
          id: 'model-b',
          name: 'Model B',
          config: {
            parameters: [
              {
                key: 'model.temperature',
                label: 'Temperature',
                type: 'number',
                required: true,
                default: '0.9',
              },
            ],
          },
        },
        {
          id: 'model-c',
          name: 'Model C',
          config: {
            parameters: [
              {
                key: 'model.top_p',
                label: 'Top P',
                type: 'number',
                required: true,
                default: '0.42',
              },
            ],
          },
        },
      ];
    }
    if (filename === 'languages.json') {
      return [
        { code: 'en', name: 'English' },
        { code: 'fr', name: 'French' },
      ];
    }
    return [];
  };

  return {
    ...actual,
    loadProviderData: mockedLoadProviderData,
    resolveCategoryParameters: (
      provider: string,
      _category: string,
      config: any,
      currentMetadata: any[] = [],
    ) => {
      const modelParam = (config.parameters || []).find(
        (p: any) => p.key === 'model.id' && p.type === 'dropdown' && p.data,
      );
      if (!modelParam) return config.parameters || [];

      const selectedId =
        currentMetadata.find((m: any) => m.getKey() === modelParam.key)?.getValue() ||
        modelParam.default ||
        '';
      const models = mockedLoadProviderData(provider, modelParam.data);
      const selected = models.find(
        (model: any) => model.id === selectedId || model.name === selectedId,
      );
      const modelParams = selected?.config?.parameters;
      if (!Array.isArray(modelParams) || modelParams.length === 0) {
        return config.parameters || [];
      }
      return [modelParam, ...modelParams];
    },
  };
});

function createMetadata(key: string, value: string): Metadata {
  const m = new Metadata();
  m.setKey(key);
  m.setValue(value);
  return m;
}

describe('ConfigRenderer', () => {
  const mockOnChange = jest.fn();

  beforeEach(() => {
    mockOnChange.mockClear();
  });

  describe('dropdown fields', () => {
    const dropdownConfig: CategoryConfig = {
      preservePrefix: 'microphone.',
      parameters: [
        {
          key: 'listen.model',
          label: 'Model',
          type: 'dropdown',
          required: true,
          data: 'models.json',
          valueField: 'id',
        },
      ],
    };

    it('renders dropdown with label', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="stt"
          config={dropdownConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Model')).toBeInTheDocument();
    });

    it('renders dropdown placeholder', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="stt"
          config={dropdownConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getAllByText('Select model').length).toBeGreaterThan(0);
    });
  });

  describe('slider fields', () => {
    const sliderConfig: CategoryConfig = {
      parameters: [
        {
          key: 'listen.threshold',
          label: 'Threshold',
          type: 'slider',
          default: '0.5',
          min: 0.1,
          max: 0.9,
          step: 0.1,
          helpText: 'Set the confidence threshold.',
        },
      ],
    };

    it('renders slider with label and help text', () => {
      const params = [createMetadata('listen.threshold', '0.5')];
      render(
        <ConfigRenderer
          provider="test"
          category="stt"
          config={sliderConfig}
          parameters={params}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Threshold')).toBeInTheDocument();
      expect(screen.getByText('Set the confidence threshold.')).toBeInTheDocument();
    });

    it('renders slider control with current value', () => {
      const params = [createMetadata('listen.threshold', '0.5')];
      render(
        <ConfigRenderer
          provider="test"
          category="stt"
          config={sliderConfig}
          parameters={params}
          onParameterChange={mockOnChange}
        />,
      );

      const slider = screen.getByRole('slider') as HTMLInputElement;
      expect(slider).toBeInTheDocument();
      expect(slider).toHaveAttribute('type', 'range');
      expect(slider.value).toBe('0.5');
    });

    it('calls onParameterChange when slider value changes', () => {
      const params = [createMetadata('listen.threshold', '0.5')];
      render(
        <ConfigRenderer
          provider="test"
          category="stt"
          config={sliderConfig}
          parameters={params}
          onParameterChange={mockOnChange}
        />,
      );

      const slider = screen.getByRole('slider');
      fireEvent.change(slider, { target: { value: '0.7' } });
      expect(mockOnChange).toHaveBeenCalledTimes(1);

      const updatedParams = mockOnChange.mock.calls[0][0] as Metadata[];
      const threshold = updatedParams.find(m => m.getKey() === 'listen.threshold');
      expect(threshold?.getValue()).toBe('0.7');
    });

    it('keeps explicit 0 value instead of falling back to min', () => {
      const zeroConfig: CategoryConfig = {
        parameters: [
          {
            key: 'model.penalty',
            label: 'Penalty',
            type: 'slider',
            min: -2,
            max: 2,
            step: 0.1,
          },
        ],
      };
      const params = [createMetadata('model.penalty', '0')];

      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={zeroConfig}
          parameters={params}
          onParameterChange={mockOnChange}
        />,
      );

      const slider = screen.getByRole('slider') as HTMLInputElement;
      expect(slider.value).toBe('0');
    });
  });

  describe('number fields', () => {
    const numberConfig: CategoryConfig = {
      parameters: [
        {
          key: 'model.max_tokens',
          label: 'Max Tokens',
          type: 'number',
          default: '2048',
          min: 1,
          placeholder: 'Enter max tokens',
        },
      ],
    };

    it('renders number input with label', () => {
      const params = [createMetadata('model.max_tokens', '2048')];
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={numberConfig}
          parameters={params}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Max Tokens')).toBeInTheDocument();
      expect(screen.getByDisplayValue('2048')).toBeInTheDocument();
    });
  });

  describe('input fields', () => {
    const inputConfig: CategoryConfig = {
      parameters: [
        {
          key: 'model.endpoint',
          label: 'Endpoint URL',
          type: 'input',
          placeholder: 'Enter endpoint URL',
        },
      ],
    };

    it('renders text input with label and placeholder', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={inputConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Endpoint URL')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Enter endpoint URL')).toBeInTheDocument();
    });

    it('calls onParameterChange when input changes', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={inputConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      const input = screen.getByPlaceholderText('Enter endpoint URL');
      fireEvent.change(input, { target: { value: 'https://api.example.com' } });
      expect(mockOnChange).toHaveBeenCalledTimes(1);

      const updatedParams = mockOnChange.mock.calls[0][0] as Metadata[];
      const endpoint = updatedParams.find(m => m.getKey() === 'model.endpoint');
      expect(endpoint?.getValue()).toBe('https://api.example.com');
    });
  });

  describe('textarea fields', () => {
    const textareaConfig: CategoryConfig = {
      parameters: [
        {
          key: 'listen.keywords',
          label: 'Keywords',
          type: 'textarea',
          required: false,
          placeholder: 'Enter keywords',
          helpText: 'Separate keywords with spaces.',
          colSpan: 2,
        },
      ],
    };

    it('renders textarea with label, placeholder, and help text', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="stt"
          config={textareaConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Keywords')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Enter keywords')).toBeInTheDocument();
      expect(screen.getByText('Separate keywords with spaces.')).toBeInTheDocument();
    });
  });

  describe('select fields', () => {
    const selectConfig: CategoryConfig = {
      parameters: [
        {
          key: 'model.reasoning_effort',
          label: 'Reasoning Effort',
          type: 'select',
          required: false,
          choices: [
            { label: 'Low', value: 'low' },
            { label: 'Medium', value: 'medium' },
            { label: 'High', value: 'high' },
          ],
        },
      ],
    };

    it('renders select with label and options', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={selectConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Reasoning Effort')).toBeInTheDocument();
    });

    it('calls onParameterChange on select change', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={selectConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      const select = screen.getByRole('combobox');
      fireEvent.change(select, { target: { value: 'high' } });
      expect(mockOnChange).toHaveBeenCalledTimes(1);

      const updatedParams = mockOnChange.mock.calls[0][0] as Metadata[];
      const effort = updatedParams.find(m => m.getKey() === 'model.reasoning_effort');
      expect(effort?.getValue()).toBe('high');
    });
  });

  describe('json fields', () => {
    const jsonConfig: CategoryConfig = {
      parameters: [
        {
          key: 'model.response_format',
          label: 'Response Format',
          type: 'json',
          required: false,
        },
      ],
    };

    it('renders textarea with JSON placeholder', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={jsonConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Response Format')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Enter as JSON')).toBeInTheDocument();
    });

    it('updates json field as raw string without parsing', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={jsonConfig}
          parameters={[createMetadata('model.response_format', '')]}
          onParameterChange={mockOnChange}
        />,
      );

      const input = screen.getByPlaceholderText('Enter as JSON');
      fireEvent.change(input, { target: { value: '{' } });

      expect(mockOnChange).toHaveBeenCalled();
      const updated = mockOnChange.mock.calls[
        mockOnChange.mock.calls.length - 1
      ][0] as Metadata[];
      const responseFormat = updated.find(m => m.getKey() === 'model.response_format');
      expect(responseFormat?.getValue()).toBe('{');
    });

    it('updates target json key without mutating sibling json parameter', () => {
      const thinkingConfig: CategoryConfig = {
        parameters: [
          {
            key: 'model.response_format',
            label: 'Response Format',
            type: 'json',
            required: false,
          },
          {
            key: 'model.thinking',
            label: 'Thinking',
            type: 'json',
            required: false,
          },
        ],
      };
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={thinkingConfig}
          parameters={[
            createMetadata('model.response_format', '{"type":"json_object"}'),
            createMetadata('model.thinking', '{"budget_tokens":100}'),
          ]}
          onParameterChange={mockOnChange}
        />,
      );

      const jsonTextareas = screen.getAllByPlaceholderText('Enter as JSON');
      fireEvent.change(jsonTextareas[1], {
        target: { value: '{"budget_tokens":250}' },
      });

      expect(mockOnChange).toHaveBeenCalled();
      const updated = mockOnChange.mock.calls[
        mockOnChange.mock.calls.length - 1
      ][0] as Metadata[];
      const thinking = updated.find(m => m.getKey() === 'model.thinking');
      const responseFormat = updated.find(m => m.getKey() === 'model.response_format');
      expect(thinking?.getValue()).toBe('{"budget_tokens":250}');
      expect(responseFormat?.getValue()).toBe('{"type":"json_object"}');
    });
  });

  describe('showWhen conditional visibility', () => {
    const conditionalConfig: CategoryConfig = {
      parameters: [
        {
          key: 'model.id',
          label: 'Model',
          type: 'input',
          required: true,
        },
        {
          key: 'model.reasoning_effort',
          label: 'Reasoning Effort',
          type: 'select',
          required: false,
          choices: [
            { label: 'Low', value: 'low' },
            { label: 'High', value: 'high' },
          ],
          showWhen: { key: 'model.id', pattern: '^o' },
        },
      ],
    };

    it('hides field when showWhen condition not met', () => {
      const params = [createMetadata('model.id', 'gpt-4')];
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={conditionalConfig}
          parameters={params}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Model')).toBeInTheDocument();
      expect(screen.queryByText('Reasoning Effort')).not.toBeInTheDocument();
    });

    it('shows field when showWhen condition is met', () => {
      const params = [createMetadata('model.id', 'o1-preview')];
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={conditionalConfig}
          parameters={params}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Model')).toBeInTheDocument();
      expect(screen.getByText('Reasoning Effort')).toBeInTheDocument();
    });
  });

  describe('advanced params (text category with popover)', () => {
    const advancedConfig: CategoryConfig = {
      parameters: [
        {
          key: 'model.id',
          label: 'Model',
          type: 'dropdown',
          required: true,
          data: 'models.json',
          valueField: 'id',
        },
        {
          key: 'model.temperature',
          label: 'Temperature',
          type: 'slider',
          default: '0.7',
          min: 0,
          max: 1,
          step: 0.1,
          advanced: true,
        },
      ],
    };

    it('renders the advanced toggle button for text category', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={advancedConfig}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      // Should render the bolt/x toggle button
      const button = screen.getByRole('button');
      expect(button).toBeInTheDocument();
    });
  });

  describe('model-level overrides', () => {
    const modelAwareConfig: CategoryConfig = {
      parameters: [
        {
          key: 'model.id',
          label: 'Model',
          type: 'dropdown',
          required: true,
          default: 'model-a',
          data: 'models.json',
          valueField: 'id',
          linkedField: {
            key: 'model.name',
            sourceField: 'name',
          },
        },
        {
          key: 'model.temperature',
          label: 'Temperature',
          type: 'number',
          required: true,
          default: '0.7',
        },
        {
          key: 'model.seed',
          label: 'Seed',
          type: 'number',
          required: false,
        },
      ],
    };

    it('renders only selector + model-defined parameters for selected model', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={modelAwareConfig}
          parameters={[
            createMetadata('model.id', 'model-a'),
            createMetadata('model.name', 'Model A'),
          ]}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Model')).toBeInTheDocument();
      expect(screen.queryByText('Seed')).not.toBeInTheDocument();
    });

    it('hydrates model-specific defaults when model dropdown changes', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={modelAwareConfig}
          parameters={[
            createMetadata('model.id', 'model-a'),
            createMetadata('model.name', 'Model A'),
          ]}
          onParameterChange={mockOnChange}
        />,
      );

      const dropdown = screen.getAllByRole('combobox')[0];
      fireEvent.change(dropdown, { target: { value: 'model-b' } });

      expect(mockOnChange).toHaveBeenCalled();
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0] as Metadata[];
      const temperature = lastCall.find(m => m.getKey() === 'model.temperature');
      const modelId = lastCall.find(m => m.getKey() === 'model.id');
      expect(modelId?.getValue()).toBe('model-b');
      expect(temperature?.getValue()).toBe('0.9');
    });

    it('drops stale model-specific keys when switched model exposes a different schema', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={modelAwareConfig}
          parameters={[
            createMetadata('model.id', 'model-a'),
            createMetadata('model.name', 'Model A'),
            createMetadata('model.temperature', '0.55'),
          ]}
          onParameterChange={mockOnChange}
        />,
      );

      const dropdown = screen.getAllByRole('combobox')[0];
      fireEvent.change(dropdown, { target: { value: 'model-c' } });

      expect(mockOnChange).toHaveBeenCalled();
      const lastCall = mockOnChange.mock.calls[
        mockOnChange.mock.calls.length - 1
      ][0] as Metadata[];

      expect(lastCall.find(m => m.getKey() === 'model.id')?.getValue()).toBe(
        'model-c',
      );
      expect(lastCall.find(m => m.getKey() === 'model.top_p')?.getValue()).toBe(
        '0.42',
      );
      expect(lastCall.find(m => m.getKey() === 'model.temperature')).toBeUndefined();
    });

    it('supports model switch followed by parameter edit', () => {
      const { rerender } = render(
        <ConfigRenderer
          provider="test"
          category="text"
          config={modelAwareConfig}
          parameters={[
            createMetadata('model.id', 'model-a'),
            createMetadata('model.name', 'Model A'),
          ]}
          onParameterChange={mockOnChange}
        />,
      );

      const dropdown = screen.getAllByRole('combobox')[0];
      fireEvent.change(dropdown, { target: { value: 'model-b' } });

      const switchedParams = mockOnChange.mock.calls[
        mockOnChange.mock.calls.length - 1
      ][0] as Metadata[];
      rerender(
        <ConfigRenderer
          provider="test"
          category="text"
          config={modelAwareConfig}
          parameters={switchedParams}
          onParameterChange={mockOnChange}
        />,
      );

      const numberInput = screen.getByRole('spinbutton') as HTMLInputElement;
      fireEvent.change(numberInput, { target: { value: '1.1' } });

      const updated = mockOnChange.mock.calls[
        mockOnChange.mock.calls.length - 1
      ][0] as Metadata[];
      expect(updated.find(m => m.getKey() === 'model.id')?.getValue()).toBe(
        'model-b',
      );
      expect(
        updated.find(m => m.getKey() === 'model.temperature')?.getValue(),
      ).toBe('1.1');
    });
  });

  describe('empty/null parameters', () => {
    const config: CategoryConfig = {
      parameters: [
        {
          key: 'listen.model',
          label: 'Model',
          type: 'input',
          required: true,
        },
      ],
    };

    it('handles null parameters gracefully', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="stt"
          config={config}
          parameters={null}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Model')).toBeInTheDocument();
    });

    it('handles empty parameters array', () => {
      render(
        <ConfigRenderer
          provider="test"
          category="stt"
          config={config}
          parameters={[]}
          onParameterChange={mockOnChange}
        />,
      );

      expect(screen.getByText('Model')).toBeInTheDocument();
    });
  });
});
