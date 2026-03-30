import React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

import { ProviderDropdown } from '@/app/components/dropdown/provider-dropdown';

const mockDropdown = jest.fn();

jest.mock('@/providers', () => ({
  INTEGRATION_PROVIDER: [
    {
      code: 'openai',
      name: 'OpenAI',
      image: '/openai.png',
      featureList: ['external'],
    },
    {
      code: 'anthropic',
      name: 'Anthropic',
      image: '/anthropic.png',
      featureList: ['external'],
    },
  ],
}));

jest.mock('@/app/components/dropdown', () => ({
  Dropdown: (props: any) => {
    mockDropdown(props);
    return (
      <div data-testid="provider-dropdown">
        <div data-testid="provider-current">{props.label(props.currentValue)}</div>
        <div data-testid="provider-options">
          {props.allValue.map((p: any) => (
            <div key={p.code}>{props.option(p, false)}</div>
          ))}
        </div>
      </div>
    );
  },
}));

describe('ProviderDropdown', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('passes integration providers to dropdown with expected wiring', () => {
    const setCurrentProvider = jest.fn();
    const currentProvider = {
      code: 'openai',
      name: 'OpenAI',
      image: '/openai.png',
      featureList: ['external'],
    };

    render(
      <ProviderDropdown
        currentProvider={currentProvider as any}
        setCurrentProvider={setCurrentProvider as any}
      />,
    );

    expect(mockDropdown).toHaveBeenCalledWith(
      expect.objectContaining({
        currentValue: currentProvider,
        setValue: setCurrentProvider,
        allValue: expect.arrayContaining([
          expect.objectContaining({ code: 'openai', name: 'OpenAI' }),
          expect.objectContaining({ code: 'anthropic', name: 'Anthropic' }),
        ]),
        placeholder: 'Select the provider',
      }),
    );
  });

  it('renders provider listing labels with icon and name for current + options', () => {
    const setCurrentProvider = jest.fn();
    const currentProvider = {
      code: 'openai',
      name: 'OpenAI',
      image: '/openai.png',
      featureList: ['external'],
    };

    render(
      <ProviderDropdown
        currentProvider={currentProvider as any}
        setCurrentProvider={setCurrentProvider as any}
      />,
    );

    expect(screen.getAllByAltText('OpenAI').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByAltText('Anthropic').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText(/OpenAI|Anthropic/).length).toBeGreaterThanOrEqual(2);
  });
});
