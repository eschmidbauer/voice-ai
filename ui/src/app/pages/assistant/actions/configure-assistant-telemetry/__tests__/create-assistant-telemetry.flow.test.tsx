import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { CreateAssistantTelemetry } from '@/app/pages/assistant/actions/configure-assistant-telemetry/create-assistant-telemetry';
import { CreateAssistantTelemetryProvider, Metadata } from '@rapidaai/react';
import {
  GetDefaultTelemetryIfInvalid,
  ValidateTelemetry,
} from '@/app/components/providers/telemetry/provider';

const mockShowLoader = jest.fn();
const mockHideLoader = jest.fn();
const mockNavigator = {
  goBack: jest.fn(),
  goToAssistantTelemetry: jest.fn(),
};

const meta = (key: string, value: string): Metadata => {
  const m = new Metadata();
  m.setKey(key);
  m.setValue(value);
  return m;
};

jest.mock('@rapidaai/react', () => {
  class ConnectionConfig {
    constructor(_: unknown) {}
    static WithDebugger(config: unknown) {
      return config;
    }
  }
  class Metadata {
    private key = '';
    private value = '';
    setKey(v: string) {
      this.key = v;
    }
    getKey() {
      return this.key;
    }
    setValue(v: string) {
      this.value = v;
    }
    getValue() {
      return this.value;
    }
  }
  return {
    ConnectionConfig,
    Metadata,
    CreateAssistantTelemetryProvider: jest.fn(),
  };
});

jest.mock('@/providers', () => ({
  TELEMETRY_PROVIDER: [
    { code: 'otlp_http', name: 'OTLP HTTP' },
    { code: 'otlp_grpc', name: 'OTLP gRPC' },
  ],
}));

jest.mock('@/hooks', () => ({
  useRapidaStore: () => ({
    loading: false,
    showLoader: mockShowLoader,
    hideLoader: mockHideLoader,
  }),
}));

jest.mock('@/hooks/use-credential', () => ({
  useCurrentCredential: () => ({ authId: 'u1', token: 't1', projectId: 'p1' }),
}));

jest.mock('@/hooks/use-global-navigator', () => ({
  useGlobalNavigation: () => mockNavigator,
}));

jest.mock('@/app/pages/assistant/actions/hooks/use-confirmation', () => ({
  useConfirmDialog: () => ({
    showDialog: (cb: () => void) => cb(),
    ConfirmDialogComponent: () => null,
  }),
}));

jest.mock('@/app/components/providers/telemetry/provider', () => ({
  GetDefaultTelemetryIfInvalid: jest.fn(),
  ValidateTelemetry: jest.fn(),
}));

jest.mock('@/app/components/providers/telemetry', () => ({
  TelemetryProvider: ({ onChangeProvider }: any) => (
    <button type="button" onClick={() => onChangeProvider('otlp_grpc')}>
      Switch provider
    </button>
  ),
}));

jest.mock('@/app/components/carbon/button', () => ({
  PrimaryButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
  SecondaryButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
}));

jest.mock('@/app/components/carbon/form', () => ({
  Stack: ({ children }: any) => <div>{children}</div>,
}));

jest.mock('@/app/components/carbon/notification', () => ({
  Notification: ({ subtitle }: any) => <div>{subtitle}</div>,
}));

jest.mock('@/app/components/form/checkbox', () => ({
  InputCheckbox: ({ checked, onChange, children }: any) => (
    <label>
      <input type="checkbox" checked={checked} onChange={onChange} />
      {children}
    </label>
  ),
}));

jest.mock('@carbon/react', () => ({
  ButtonSet: ({ children }: any) => <div>{children}</div>,
  Breadcrumb: ({ children }: any) => <div>{children}</div>,
  BreadcrumbItem: ({ children }: any) => <div>{children}</div>,
}));

jest.mock('react-hot-toast/headless', () => ({
  success: jest.fn(),
  error: jest.fn(),
}));

describe('Create assistant telemetry flow', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    (GetDefaultTelemetryIfInvalid as jest.Mock).mockImplementation(
      (provider: string, parameters: Metadata[]) => [
        ...(parameters || []),
        meta('telemetry.provider', provider),
      ],
    );
    (ValidateTelemetry as jest.Mock).mockReturnValue(undefined);
    (CreateAssistantTelemetryProvider as jest.Mock).mockImplementation(
      (
        _cfg,
        _assistantId,
        _provider,
        _enabled,
        _params,
        cb,
      ) => cb(null, { getSuccess: () => true }),
    );
  });

  it('shows validation error when telemetry config is invalid', () => {
    (ValidateTelemetry as jest.Mock).mockReturnValue(
      'Please provide a valid telemetry endpoint.',
    );
    render(<CreateAssistantTelemetry assistantId="assistant-1" />);

    fireEvent.click(screen.getByRole('button', { name: 'Save telemetry' }));

    expect(
      screen.getByText('Please provide a valid telemetry endpoint.'),
    ).toBeInTheDocument();
    expect(CreateAssistantTelemetryProvider).not.toHaveBeenCalled();
  });

  it('switching provider rehydrates defaults from credential-only parameters', () => {
    (GetDefaultTelemetryIfInvalid as jest.Mock)
      .mockReturnValueOnce([
        meta('rapida.credential_id', 'cred-1'),
        meta('telemetry.endpoint', 'https://old-endpoint'),
      ])
      .mockImplementation((_provider: string, params: Metadata[]) => params);

    render(<CreateAssistantTelemetry assistantId="assistant-1" />);
    fireEvent.click(screen.getByRole('button', { name: 'Switch provider' }));

    expect(GetDefaultTelemetryIfInvalid).toHaveBeenNthCalledWith(
      2,
      'otlp_grpc',
      [expect.objectContaining({})],
    );
    const params = (GetDefaultTelemetryIfInvalid as jest.Mock).mock.calls[1][1] as Metadata[];
    expect(params).toHaveLength(1);
    expect(params[0].getKey()).toBe('rapida.credential_id');
    expect(params[0].getValue()).toBe('cred-1');
  });

  it('creates telemetry provider successfully and navigates back to telemetry listing', async () => {
    render(<CreateAssistantTelemetry assistantId="assistant-1" />);

    fireEvent.click(screen.getByRole('button', { name: 'Save telemetry' }));

    await waitFor(() => {
      expect(CreateAssistantTelemetryProvider).toHaveBeenCalledTimes(1);
    });

    expect(CreateAssistantTelemetryProvider).toHaveBeenCalledWith(
      expect.anything(),
      'assistant-1',
      'otlp_http',
      true,
      expect.any(Array),
      expect.any(Function),
      expect.objectContaining({
        'x-auth-id': 'u1',
        authorization: 't1',
        'x-project-id': 'p1',
      }),
    );
    expect(mockShowLoader).toHaveBeenCalled();
    expect(mockHideLoader).toHaveBeenCalled();
    expect(mockNavigator.goToAssistantTelemetry).toHaveBeenCalledWith(
      'assistant-1',
    );
  });
});
