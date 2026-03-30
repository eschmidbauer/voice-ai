import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { APiStringHeader } from '@/app/components/external-api/api-header';
import { ParameterEditor } from '@/app/components/tools/common/components';

jest.mock('@/utils', () => ({
  cn: (...inputs: string[]) => inputs.filter(Boolean).join(' '),
}));

jest.mock('@/app/components/carbon/form', () => {
  const React = require('react');
  return {
    TextInput: ({ id, value, onChange, placeholder, labelText, hideLabel }: any) =>
      React.createElement(
        'div',
        null,
        !hideLabel && labelText
          ? React.createElement('label', { htmlFor: id }, labelText)
          : null,
        React.createElement('input', {
          id,
          'data-testid': id,
          value: value ?? '',
          placeholder,
          onChange,
        }),
      ),
    TextArea: ({ id, value, onChange }: any) =>
      React.createElement('textarea', { id, value: value ?? '', onChange }),
    Stack: ({ children }: any) => React.createElement('div', null, children),
  };
});

jest.mock('@/app/components/carbon/button', () => {
  const React = require('react');
  return {
    TertiaryButton: ({ children, onClick }: any) =>
      React.createElement('button', { onClick }, children),
  };
});

jest.mock('@/app/components/container/message/notice-block/doc-notice-block', () => {
  const React = require('react');
  return {
    DocNoticeBlock: ({ children }: any) => React.createElement('div', null, children),
  };
});

jest.mock('@/app/components/form/editor/code-editor', () => {
  const React = require('react');
  return {
    CodeEditor: ({ value, onChange }: any) =>
      React.createElement('textarea', {
        'aria-label': 'Code Editor',
        value: value ?? '',
        onChange: (e: any) => onChange(e.target.value),
      }),
  };
});

jest.mock('@carbon/react', () => {
  const React = require('react');
  return {
    Select: ({ id, value, onChange, children, hideLabel, labelText }: any) =>
      React.createElement(
        'div',
        null,
        !hideLabel && labelText
          ? React.createElement('label', { htmlFor: id }, labelText)
          : null,
        React.createElement(
          'select',
          { id, 'data-testid': id, value, onChange },
          children,
        ),
      ),
    SelectItem: ({ value, text }: any) =>
      React.createElement('option', { value }, text),
    Button: ({ children, onClick, iconDescription }: any) =>
      React.createElement(
        'button',
        { onClick, 'aria-label': iconDescription || children || 'button' },
        children || 'button',
      ),
  };
});

describe('Tool list editors', () => {
  it('header list supports add/delete and serializes JSON payload', async () => {
    const onChange = jest.fn();

    render(
      <APiStringHeader
        headerValue='{"Authorization":"Bearer token"}'
        setHeaderValue={onChange}
      />,
    );

    await waitFor(() => {
      expect(screen.getByDisplayValue('Authorization')).toBeInTheDocument();
      expect(screen.getByDisplayValue('Bearer token')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Add header' }));
    fireEvent.change(screen.getByTestId('api-header-key-1'), {
      target: { value: 'X-Trace' },
    });
    fireEvent.change(screen.getByTestId('api-header-val-1'), {
      target: { value: 'trace-id' },
    });

    const setPayloads = onChange.mock.calls.map(call => JSON.parse(call[0]));
    expect(setPayloads).toContainEqual({
      Authorization: 'Bearer token',
      'X-Trace': 'trace-id',
    });

    fireEvent.click(screen.getAllByRole('button', { name: 'Remove' })[0]);

    const lastCallArg =
      onChange.mock.calls[onChange.mock.calls.length - 1]?.[0] || '{}';
    expect(JSON.parse(lastCallArg)).toEqual({
      'X-Trace': 'trace-id',
    });
  });

  it('header list handles invalid json input fallback', () => {
    const spy = jest.spyOn(console, 'error').mockImplementation(() => {});
    const onChange = jest.fn();

    render(<APiStringHeader headerValue="not-json" setHeaderValue={onChange} />);

    expect(screen.getByTestId('api-header-key-0')).toBeInTheDocument();
    expect(screen.getByTestId('api-header-val-0')).toBeInTheDocument();

    spy.mockRestore();
  });

  it('mapping list supports add/edit/delete for tool parameter mapping', () => {
    const onChange = jest.fn();

    render(<ParameterEditor value="{}" onChange={onChange} />);

    expect(screen.getByText('Mapping (0)')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Add parameter' }));
    expect(screen.getByText('Mapping (1)')).toBeInTheDocument();

    fireEvent.change(screen.getByTestId('param-type-0'), {
      target: { value: 'tool' },
    });
    fireEvent.change(screen.getByTestId('key-tool-'), {
      target: { value: 'name' },
    });
    fireEvent.change(screen.getByTestId('param-val-0'), {
      target: { value: 'customer_profile' },
    });

    expect(onChange).toHaveBeenLastCalledWith(
      JSON.stringify({ 'tool.name': 'customer_profile' }),
    );

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));

    expect(onChange).toHaveBeenLastCalledWith('{}');
    expect(screen.getByText('Mapping (0)')).toBeInTheDocument();
  });
});
