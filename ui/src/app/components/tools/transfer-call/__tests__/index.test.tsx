import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Metadata } from '@rapidaai/react';
import { ConfigureTransferCall } from '../index';

jest.mock('@/app/components/carbon/form', () => {
  const React = require('react');
  return {
    Stack: ({ children }: any) => React.createElement('div', null, children),
    TextArea: ({ id, labelText, value, onChange, helperText, placeholder }: any) =>
      React.createElement(
        'div',
        null,
        labelText ? React.createElement('label', { htmlFor: id }, labelText) : null,
        helperText ? React.createElement('small', null, helperText) : null,
        React.createElement('textarea', {
          id,
          'data-testid': id,
          value: value ?? '',
          onChange,
          placeholder,
        }),
      ),
  };
});

jest.mock('@carbon/react', () => {
  const React = require('react');
  return {
    Slider: ({ id, labelText, value, onChange, min, max, step }: any) =>
      React.createElement(
        'div',
        null,
        labelText ? React.createElement('label', { htmlFor: id }, labelText) : null,
        React.createElement('input', {
          id,
          type: 'range',
          min,
          max,
          step,
          value: value ?? 0,
          'data-testid': id,
          onChange: (e: any) => onChange?.({ value: Number(e.target.value) }),
        }),
      ),
  };
});

jest.mock('@/app/components/input-group', () => {
  const React = require('react');
  return {
    InputGroup: ({ title, children }: any) =>
      React.createElement(
        'section',
        null,
        title ? React.createElement('h3', null, title) : null,
        children,
      ),
  };
});

jest.mock('@/app/components/configuration/config-var/config-select', () => {
  const React = require('react');
  return ({ options, label }: any) =>
    React.createElement(
      'div',
      { 'data-testid': 'transfer-destination-select' },
      `${label}: ${(options || []).join(',')}`,
    );
});

const createMetadata = (key: string, value: string): Metadata => {
  const metadata = new Metadata();
  metadata.setKey(key);
  metadata.setValue(value);
  return metadata;
};

describe('ConfigureTransferCall', () => {
  it('loads and updates transfer message and transfer delay for save payload', () => {
    const onParameterChange = jest.fn();
    const params = [
      createMetadata('tool.transfer_to', '+14155551234'),
      createMetadata('tool.transfer_message', 'Please hold while I transfer your call.'),
      createMetadata('tool.transfer_delay', '300'),
    ];

    render(
      <ConfigureTransferCall
        parameters={params}
        onParameterChange={onParameterChange}
      />,
    );

    const message = screen.getByTestId('transfer-message');
    const delay = screen.getByTestId('transfer-delay');

    expect(message).toHaveValue('Please hold while I transfer your call.');
    expect(delay).toHaveValue('300');

    fireEvent.change(message, {
      target: { value: 'Connecting you now to our support specialist.' },
    });

    const afterMessageUpdate = onParameterChange.mock.calls.at(-1)?.[0] as Metadata[];
    expect(
      afterMessageUpdate.find(m => m.getKey() === 'tool.transfer_message')?.getValue(),
    ).toBe('Connecting you now to our support specialist.');
    expect(
      afterMessageUpdate.find(m => m.getKey() === 'tool.transfer_delay')?.getValue(),
    ).toBe('300');

    fireEvent.change(delay, { target: { value: '650' } });

    const afterDelayUpdate = onParameterChange.mock.calls.at(-1)?.[0] as Metadata[];
    expect(
      afterDelayUpdate.find(m => m.getKey() === 'tool.transfer_delay')?.getValue(),
    ).toBe('650');
    expect(
      afterDelayUpdate.find(m => m.getKey() === 'tool.transfer_message')?.getValue(),
    ).toBe('Please hold while I transfer your call.');
  });
});
