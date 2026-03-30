import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Metadata } from '@rapidaai/react';
import {
  TelephonyProvider,
  ValidateTelephonyOptions,
} from '@/app/components/providers/telephony';
import { TELEPHONY_PROVIDER } from '@/providers';

jest.mock('@/app/components/carbon/form', () => ({
  Stack: ({ children }: any) => <div>{children}</div>,
  TextInput: ({ id, value, onChange, placeholder }: any) => (
    <input id={id} value={value ?? ''} onChange={onChange} placeholder={placeholder} />
  ),
}));

jest.mock('@carbon/react', () => {
  const React = require('react');
  return {
    Dropdown: ({ id, label, items = [], selectedItem, onChange }: any) => (
      <select
        id={id}
        aria-label={label || 'dropdown'}
        value={selectedItem?.code || ''}
        onChange={e => {
          const selected = items.find((item: any) => item.code === e.target.value);
          onChange?.({ selectedItem: selected || null });
        }}
      >
        <option value="">Select</option>
        {items.map((item: any) => (
          <option key={item.code} value={item.code}>
            {item.name}
          </option>
        ))}
      </select>
    ),
  };
});

jest.mock('@/app/components/dropdown/credential-dropdown', () => ({
  CredentialDropdown: ({ onChangeCredential }: any) => (
    <button
      type="button"
      onClick={() => onChangeCredential({ getId: () => 'cred-1' })}
    >
      Pick credential
    </button>
  ),
}));

jest.mock('@/app/components/providers/telephony/twilio', () => ({
  ConfigureTwilioTelephony: () => <div>twilio-config</div>,
  ValidateTwilioTelephonyOptions: jest.requireActual(
    '@/app/components/providers/telephony/twilio',
  ).ValidateTwilioTelephonyOptions,
}));
jest.mock('@/app/components/providers/telephony/vonage', () => ({
  ConfigureVonageTelephony: () => <div>vonage-config</div>,
  ValidateVonageTelephonyOptions: jest.requireActual(
    '@/app/components/providers/telephony/vonage',
  ).ValidateVonageTelephonyOptions,
}));
jest.mock('@/app/components/providers/telephony/exotel', () => ({
  ConfigureExotelTelephony: () => <div>exotel-config</div>,
  ValidateExotelTelephonyOptions: jest.requireActual(
    '@/app/components/providers/telephony/exotel',
  ).ValidateExotelTelephonyOptions,
}));
jest.mock('@/app/components/providers/telephony/sip', () => ({
  ConfigureSIPTelephony: () => <div>sip-config</div>,
  ValidateSIPTelephonyOptions: jest.requireActual(
    '@/app/components/providers/telephony/sip',
  ).ValidateSIPTelephonyOptions,
}));
jest.mock('@/app/components/providers/telephony/asterisk', () => ({
  ConfigureAsteriskTelephony: () => <div>asterisk-config</div>,
  ValidateAsteriskTelephonyOptions: jest.requireActual(
    '@/app/components/providers/telephony/asterisk',
  ).ValidateAsteriskTelephonyOptions,
}));

const meta = (key: string, value: string): Metadata => {
  const m = new Metadata();
  m.setKey(key);
  m.setValue(value);
  return m;
};

describe('Telephony provider runtime parity', () => {
  it('all active telephony providers are selectable', () => {
    expect(TELEPHONY_PROVIDER.length).toBeGreaterThan(0);
    for (const provider of TELEPHONY_PROVIDER) {
      expect(typeof provider.code).toBe('string');
      expect(provider.code.length).toBeGreaterThan(0);
    }
  });

  it.each([
    ['twilio', [meta('rapida.credential_id', 'cred-1'), meta('phone', '+15551234567')]],
    ['exotel', [meta('rapida.credential_id', 'cred-1'), meta('phone', '+15551234567')]],
    ['vonage', [meta('rapida.credential_id', 'cred-1'), meta('phone', '+15551234567')]],
    ['sip', [meta('rapida.credential_id', 'cred-1'), meta('phone', '+15551234567')]],
    [
      'asterisk',
      [
        meta('rapida.credential_id', 'cred-1'),
        meta('context', 'internal'),
        meta('extension', '1002'),
        meta('phone', '+15551234567'),
      ],
    ],
  ])('%s validates required telephony options', (provider, options) => {
    expect(ValidateTelephonyOptions(provider, options)).toBe(true);
  });

  it('returns false for unknown telephony provider', () => {
    expect(ValidateTelephonyOptions('unknown-telephony', [])).toBe(false);
  });

  it('updates provider and credential from TelephonyProvider UI interactions', () => {
    const onChangeProvider = jest.fn();
    const onChangeParameter = jest.fn();
    render(
      <TelephonyProvider
        provider="twilio"
        parameters={[meta('phone', '+15551234567')]}
        onChangeProvider={onChangeProvider}
        onChangeParameter={onChangeParameter}
      />,
    );

    fireEvent.change(screen.getByLabelText('Select telephony provider'), {
      target: { value: 'vonage' },
    });
    expect(onChangeProvider).toHaveBeenCalledWith('vonage');

    fireEvent.click(screen.getByRole('button', { name: 'Pick credential' }));
    expect(onChangeParameter).toHaveBeenCalled();
    const params = onChangeParameter.mock.calls[0][0] as Metadata[];
    expect(
      params.find(p => p.getKey() === 'rapida.credential_id')?.getValue(),
    ).toBe('cred-1');
  });
});
