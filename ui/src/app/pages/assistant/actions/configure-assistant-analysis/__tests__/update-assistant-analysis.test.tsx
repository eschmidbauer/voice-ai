import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';

import { UpdateAssistantAnalysis } from '@/app/pages/assistant/actions/configure-assistant-analysis/update-assistant-analysis';
import { GetAssistantAnalysis, UpdateAnalysis } from '@rapidaai/react';
import toast from 'react-hot-toast/headless';

const mockGoBack = jest.fn();
const mockGoToConfigureAssistantAnalysis = jest.fn();
const mockToastError = jest.fn();

let mockParams: Record<string, string | undefined> = { analysisId: 'analysis-1' };

jest.mock('@rapidaai/react', () => ({
  ConnectionConfig: class ConnectionConfig {
    constructor(_: unknown) {}
  },
  GetAssistantAnalysis: jest.fn(),
  UpdateAnalysis: jest.fn(),
}));

jest.mock('react-hot-toast/headless', () => ({
  __esModule: true,
  default: {
    success: jest.fn(),
    error: (...args: any[]) => mockToastError(...args),
  },
}));

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useParams: () => mockParams,
}));

jest.mock('@/hooks/use-global-navigator', () => ({
  useGlobalNavigation: () => ({
    goBack: mockGoBack,
    goToConfigureAssistantAnalysis: mockGoToConfigureAssistantAnalysis,
  }),
}));

jest.mock('@/hooks/use-credential', () => ({
  useCurrentCredential: () => ({
    authId: 'auth-1',
    token: 'token-1',
    projectId: 'project-1',
  }),
}));

jest.mock('@/utils', () => ({
  cn: (...inputs: any[]) => inputs.filter(Boolean).join(' '),
  randomMeaningfullName: () => 'analysis-default',
}));

jest.mock('@/app/pages/assistant/actions/hooks/use-confirmation', () => ({
  useConfirmDialog: () => ({
    showDialog: (cb: () => void) => cb(),
    ConfirmDialogComponent: () => null,
  }),
}));

jest.mock('@/app/components/dropdown/endpoint-dropdown', () => ({
  EndpointDropdown: ({ onChangeEndpoint }: any) => (
    <button
      type="button"
      onClick={() =>
        onChangeEndpoint({
          getId: () => 'endpoint-2',
        })
      }
    >
      Pick endpoint
    </button>
  ),
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

jest.mock('@/app/components/form/button', () => ({
  IBlueBorderButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
  IRedBorderButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
}));

jest.mock('@/app/components/carbon/button', () => ({
  PrimaryButton: ({ children, renderIcon: _renderIcon, ...props }: any) => (
    <button {...props}>{children}</button>
  ),
  SecondaryButton: ({ children, renderIcon: _renderIcon, ...props }: any) => (
    <button {...props}>{children}</button>
  ),
  TertiaryButton: ({ children, renderIcon: _renderIcon, ...props }: any) => (
    <button {...props}>{children}</button>
  ),
}));

jest.mock('@/app/components/carbon/form', () => ({
  Stack: ({ children }: any) => <div>{children}</div>,
  TextInput: ({ id, labelText, value, onChange, type = 'text', ...rest }: any) => (
    <div>
      {labelText ? <label htmlFor={id}>{labelText}</label> : null}
      <input id={id} value={value ?? ''} onChange={onChange} type={type} {...rest} />
    </div>
  ),
  TextArea: ({ id, labelText, value, onChange, ...rest }: any) => (
    <div>
      {labelText ? <label htmlFor={id}>{labelText}</label> : null}
      <textarea id={id} value={value ?? ''} onChange={onChange} {...rest} />
    </div>
  ),
}));

jest.mock('@carbon/react', () => ({
  ButtonSet: ({ children }: any) => <div>{children}</div>,
  Select: ({ id, value, onChange, children, labelText, hideLabel }: any) => (
    <div>
      {!hideLabel && labelText ? <label htmlFor={id}>{labelText}</label> : null}
      <select id={id} value={value} onChange={onChange}>
        {children}
      </select>
    </div>
  ),
  SelectItem: ({ value, text }: any) => <option value={value}>{text}</option>,
  Button: ({ children, iconDescription, ...props }: any) => (
    <button aria-label={iconDescription} {...props}>
      {children}
    </button>
  ),
  NumberInput: ({ id, value, onChange, label, hideLabel }: any) => (
    <div>
      {!hideLabel && label ? <label htmlFor={id}>{label}</label> : null}
      <input
        id={id}
        type="number"
        value={value ?? ''}
        onChange={e => onChange?.(e, { value: e.target.value })}
      />
    </div>
  ),
}));

jest.mock('@/app/components/form/fieldset', () => ({
  FieldSet: ({ children }: any) => <div>{children}</div>,
}));

jest.mock('@/app/components/form-label', () => ({
  FormLabel: ({ children }: any) => <label>{children}</label>,
}));

jest.mock('@/app/components/form/input', () => ({
  Input: ({ ...props }: any) => <input {...props} />,
}));

jest.mock('@/app/components/form/select', () => ({
  Select: ({ options = [], value, onChange }: any) => (
    <select value={value} onChange={onChange}>
      {options.map((o: any) => (
        <option key={o.value} value={o.value}>
          {o.name}
        </option>
      ))}
    </select>
  ),
}));

jest.mock('@/app/components/form/textarea', () => ({
  Textarea: ({ ...props }: any) => <textarea {...props} />,
}));

jest.mock('@/app/components/input-helper', () => ({
  InputHelper: ({ children }: any) => <span>{children}</span>,
}));

jest.mock('@/app/components/blocks/section-divider', () => ({
  SectionDivider: ({ label }: any) => <h3>{label}</h3>,
}));

jest.mock('lucide-react', () => ({
  ArrowRight: () => <span>arrow-right</span>,
  Plus: () => <span>plus</span>,
  Trash2: () => <span>trash</span>,
}));

describe('UpdateAssistantAnalysis', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockParams = { analysisId: 'analysis-1' };
    (GetAssistantAnalysis as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _analysisId, callback) => {
        callback(null, {
          getData: () => ({
            getName: () => 'loaded-analysis',
            getDescription: () => 'loaded-description',
            getExecutionpriority: () => 3,
            getEndpointid: () => 'endpoint-1',
            getEndpointparametersMap: () =>
              new Map<string, string>([['conversation.messages', 'messages']]),
          }),
        });
      },
    );
  });

  it('loads analysis on mount and pre-fills fields', async () => {
    render(<UpdateAssistantAnalysis assistantId="assistant-1" />);

    await waitFor(() => {
      expect(GetAssistantAnalysis).toHaveBeenCalled();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    expect(screen.getByDisplayValue('loaded-analysis')).toBeInTheDocument();
  });

  it('shows validation error when configure tab has no parameters', async () => {
    render(<UpdateAssistantAnalysis assistantId="assistant-1" />);

    await waitFor(() => {
      expect(GetAssistantAnalysis).toHaveBeenCalled();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    expect(
      screen.getByText('Please provide one or more parameters.'),
    ).toBeInTheDocument();
  });

  it('updates analysis successfully and navigates back to analysis listing', async () => {
    (UpdateAnalysis as jest.Mock).mockImplementation(
      (
        _cfg,
        _assistantId,
        _analysisId,
        _name,
        _endpointId,
        _version,
        _priority,
        _params,
        callback,
      ) => {
        callback(null, { getSuccess: () => true });
      },
    );

    render(<UpdateAssistantAnalysis assistantId="assistant-1" />);

    await waitFor(() => {
      expect(GetAssistantAnalysis).toHaveBeenCalled();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.click(screen.getByRole('button', { name: 'Update analysis' }));

    await waitFor(() => {
      expect(UpdateAnalysis).toHaveBeenCalled();
    });
    expect(toast.success).toHaveBeenCalledWith(`Assistant's analysis updated successfully`);
    expect(mockGoToConfigureAssistantAnalysis).toHaveBeenCalledWith('assistant-1');
  });

  it('shows human error message when update response is unsuccessful', async () => {
    (UpdateAnalysis as jest.Mock).mockImplementation(
      (
        _cfg,
        _assistantId,
        _analysisId,
        _name,
        _endpointId,
        _version,
        _priority,
        _params,
        callback,
      ) => {
        callback(null, {
          getSuccess: () => false,
          getError: () => ({ getHumanmessage: () => 'Invalid analysis name' }),
        });
      },
    );

    render(<UpdateAssistantAnalysis assistantId="assistant-1" />);

    await waitFor(() => {
      expect(GetAssistantAnalysis).toHaveBeenCalled();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.click(screen.getByRole('button', { name: 'Update analysis' }));

    expect(await screen.findByText('Invalid analysis name')).toBeInTheDocument();
  });
});
