import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import {
  CreateAssistantWebhook,
} from '@/app/pages/assistant/actions/configure-assistant-webhook/create-assistant-webhook';
import {
  UpdateAssistantWebhook,
} from '@/app/pages/assistant/actions/configure-assistant-webhook/update-assistant-webhook';
import {
  CreateWebhook,
  GetAssistantWebhook,
  UpdateWebhook,
} from '@rapidaai/react';

let mockParams: Record<string, string | undefined> = {
  assistantId: 'assistant-1',
  webhookId: 'webhook-1',
};

const mockShowLoader = jest.fn();
const mockHideLoader = jest.fn();
const mockNavigate = {
  goBack: jest.fn(),
  goToAssistantWebhook: jest.fn(),
};

jest.mock('@rapidaai/react', () => ({
  ConnectionConfig: class {
    constructor(_: unknown) {}
    static WithDebugger(config: unknown) {
      return config;
    }
  },
  CreateWebhook: jest.fn(),
  GetAssistantWebhook: jest.fn(),
  UpdateWebhook: jest.fn(),
}));

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useParams: () => mockParams,
}));

jest.mock('@/hooks', () => ({
  useRapidaStore: () => ({
    loading: false,
    showLoader: mockShowLoader,
    hideLoader: mockHideLoader,
  }),
}));

jest.mock('@/hooks/use-global-navigator', () => ({
  useGlobalNavigation: () => mockNavigate,
}));

jest.mock('@/hooks/use-credential', () => ({
  useCurrentCredential: () => ({ authId: 'u1', token: 't1', projectId: 'p1' }),
}));

jest.mock('@/app/pages/assistant/actions/hooks/use-confirmation', () => ({
  useConfirmDialog: () => ({
    showDialog: (cb: () => void) => cb(),
    ConfirmDialogComponent: () => null,
  }),
}));

jest.mock('@/app/components/form/tab-form', () => ({
  TabForm: ({ form, activeTab, errorMessage, formHeading }: any) => {
    const active = form.find((f: any) => f.code === activeTab) || form[0];
    return (
      <div>
        <h1>{formHeading}</h1>
        {errorMessage ? <div>{errorMessage}</div> : null}
        <div>{active.body}</div>
        <div>{active.actions}</div>
      </div>
    );
  },
}));

jest.mock('@/app/components/carbon/form', () => {
  const React = require('react');
  return {
    Stack: ({ children }: any) => React.createElement('div', null, children),
    TextInput: ({ id, labelText, value, onChange, placeholder, hideLabel }: any) =>
      React.createElement(
        'div',
        null,
        !hideLabel && labelText
          ? React.createElement('label', { htmlFor: id }, labelText)
          : null,
        React.createElement('input', {
          id,
          value: value ?? '',
          onChange,
          placeholder,
          'data-testid': id,
        }),
      ),
    TextArea: ({ id, labelText, value, onChange, placeholder }: any) =>
      React.createElement(
        'div',
        null,
        labelText ? React.createElement('label', { htmlFor: id }, labelText) : null,
        React.createElement('textarea', {
          id,
          value: value ?? '',
          onChange,
          placeholder,
          'data-testid': id,
        }),
      ),
  };
});

jest.mock('@/app/components/form/slider', () => {
  const React = require('react');
  return {
    Slider: ({ value, onSlide }: any) =>
      React.createElement('input', {
        type: 'range',
        value,
        onChange: (e: any) => onSlide(Number(e.target.value)),
        'data-testid': 'webhook-timeout-slider',
      }),
  };
});

jest.mock('@/app/components/carbon/button', () => {
  const React = require('react');
  return {
    PrimaryButton: ({ children, ...props }: any) =>
      React.createElement('button', props, children),
    SecondaryButton: ({ children, ...props }: any) =>
      React.createElement('button', props, children),
    TertiaryButton: ({ children, ...props }: any) =>
      React.createElement('button', props, children),
  };
});

jest.mock('@carbon/react', () => {
  const React = require('react');
  return {
    ButtonSet: ({ children }: any) => React.createElement('div', null, children),
    Select: ({ id, labelText, value, onChange, children, hideLabel }: any) =>
      React.createElement(
        'div',
        null,
        !hideLabel && labelText
          ? React.createElement('label', { htmlFor: id }, labelText)
          : null,
        React.createElement('select', { id, value, onChange, 'data-testid': id }, children),
      ),
    SelectItem: ({ value, text }: any) => React.createElement('option', { value }, text),
    NumberInput: ({ id, value, onChange, label, hideLabel }: any) =>
      React.createElement(
        'div',
        null,
        !hideLabel && label ? React.createElement('label', { htmlFor: id }, label) : null,
        React.createElement('input', {
          id,
          type: 'number',
          value,
          onChange: (e: any) => onChange?.(e, { value: e.target.value }),
          'data-testid': id,
        }),
      ),
    Checkbox: ({ id, labelText, checked, onChange }: any) =>
      React.createElement(
        'label',
        { htmlFor: id },
        React.createElement('input', {
          id,
          type: 'checkbox',
          checked: !!checked,
          onChange: (e: any) => onChange?.(e, { checked: e.target.checked }),
        }),
        labelText,
      ),
    Button: ({ iconDescription, children, ...props }: any) =>
      React.createElement(
        'button',
        { ...props, 'aria-label': iconDescription || children || 'button' },
        children || 'button',
      ),
  };
});

jest.mock('react-hot-toast/headless', () => ({
  success: jest.fn(),
  error: jest.fn(),
}));

describe('Assistant webhook flows', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockParams = { assistantId: 'assistant-1', webhookId: 'webhook-1' };

    (CreateWebhook as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _method, _url, _headers, _params, _events, _retryStatus, _maxRetry, _timeout, _priority, cb) =>
        cb(null, { getSuccess: () => true }),
    );

    (GetAssistantWebhook as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _webhookId, cb) =>
        cb(null, {
          getData: () => ({
            getHttpmethod: () => 'POST',
            getHttpurl: () => 'https://hooks.example.com/incoming',
            getDescription: () => 'existing webhook',
            getRetrystatuscodesList: () => ['50X'],
            getRetrycount: () => 2,
            getTimeoutsecond: () => 200,
            getExecutionpriority: () => 1,
            getHttpheadersMap: () => new Map([['Authorization', 'Bearer token']]),
            getHttpbodyMap: () =>
              new Map([
                ['event.type', 'event'],
                ['assistant.id', 'assistant_id'],
              ]),
            getAssistanteventsList: () => ['conversation.begin'],
          }),
        }),
    );

    (UpdateWebhook as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _webhookId, _method, _url, _headers, _params, _events, _retryStatus, _maxRetry, _timeout, _priority, cb) =>
        cb(null, { getSuccess: () => true }),
    );
  });

  it('create webhook supports header/parameter add-delete and payload duplicate-key validation', () => {
    render(<CreateAssistantWebhook assistantId="assistant-1" />);

    fireEvent.change(screen.getByTestId('webhook-endpoint'), {
      target: { value: 'https://api.example.com/webhook' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    expect(screen.getByText('Headers (0)')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Add header' }));
    expect(screen.getByText('Headers (1)')).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole('button', { name: 'Remove' })[0]);
    expect(screen.getByText('Headers (0)')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Add parameter' }));
    fireEvent.change(screen.getByTestId('param-type-2'), {
      target: { value: 'event' },
    });
    fireEvent.change(screen.getAllByTestId('type-key-event')[2], {
      target: { value: 'type' },
    });
    fireEvent.change(screen.getByTestId('param-val-2'), {
      target: { value: 'duplicate_key_payload' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    expect(
      screen.getByText('Duplicate parameter keys are not allowed.'),
    ).toBeInTheDocument();
  });

  it('create webhook submits with selected event and configuration', async () => {
    render(<CreateAssistantWebhook assistantId="assistant-1" />);

    fireEvent.change(screen.getByTestId('webhook-endpoint'), {
      target: { value: 'https://api.example.com/webhook' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    fireEvent.click(screen.getByLabelText('conversation.begin'));
    fireEvent.change(screen.getByTestId('webhook-max-retries'), {
      target: { value: '2' },
    });
    fireEvent.change(screen.getByTestId('webhook-timeout'), {
      target: { value: '220' },
    });
    fireEvent.change(screen.getByTestId('webhook-priority'), {
      target: { value: '4' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Configure webhook' }));

    await waitFor(() => expect(CreateWebhook).toHaveBeenCalledTimes(1));
    expect(CreateWebhook).toHaveBeenCalledWith(
      expect.anything(),
      'assistant-1',
      'POST',
      'https://api.example.com/webhook',
      expect.any(Array),
      expect.any(Array),
      ['conversation.begin'],
      expect.any(Array),
      2,
      220,
      4,
      expect.any(Function),
      expect.any(Object),
      '',
    );
  });

  it('update webhook validates invalid destination url', async () => {
    render(<UpdateAssistantWebhook assistantId="assistant-1" />);

    await waitFor(() => {
      expect(screen.getByTestId('webhook-endpoint')).toHaveValue(
        'https://hooks.example.com/incoming',
      );
    });

    fireEvent.change(screen.getByTestId('webhook-endpoint'), {
      target: { value: 'bad-url' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    expect(
      screen.getByText('Please provide a valid server URL for the webhook.'),
    ).toBeInTheDocument();
  });

  it('update webhook submits with loaded values', async () => {
    render(<UpdateAssistantWebhook assistantId="assistant-1" />);

    await waitFor(() => {
      expect(screen.getByTestId('webhook-endpoint')).toHaveValue(
        'https://hooks.example.com/incoming',
      );
    });

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.click(screen.getByRole('button', { name: 'Update webhook' }));

    await waitFor(() => expect(UpdateWebhook).toHaveBeenCalledTimes(1));
    expect(UpdateWebhook).toHaveBeenCalledWith(
      expect.anything(),
      'assistant-1',
      'webhook-1',
      'POST',
      'https://hooks.example.com/incoming',
      expect.any(Array),
      expect.any(Array),
      ['conversation.begin'],
      ['50X'],
      2,
      200,
      1,
      expect.any(Function),
      expect.any(Object),
      'existing webhook',
    );
  });
});
