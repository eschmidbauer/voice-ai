import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

import { ForgotPasswordPage } from '@/app/pages/authentication/forgot-password';
import { CreateEndpointPage } from '@/app/pages/endpoint/actions/create-endpoint';
import { CreateNewVersionEndpointPage } from '@/app/pages/endpoint/actions/create-endpoint-version';
import { CreateAssistantPage } from '@/app/pages/assistant/actions/create-assistant';
import { CreateVersionAssistantPage } from '@/app/pages/assistant/actions/create-assistant-version';
import { ForgotPassword } from '@rapidaai/react';

let mockParams: Record<string, string | undefined> = {};
const mockNavigate = jest.fn();
const mockShowLoader = jest.fn();
const mockHideLoader = jest.fn();

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
  class EndpointProviderModelAttribute {
    setModelprovidername(_: string) {}
    setEndpointmodeloptionsList(_: unknown[]) {}
    setChatcompleteprompt(_: unknown) {}
    setDescription(_: string) {}
  }
  class EndpointAttribute {
    setName(_: string) {}
    setDescription(_: string) {}
  }
  class CreateAssistantProviderRequest {
    static CreateAssistantProviderModel = class {
      setTemplate(_: unknown) {}
      setModelprovidername(_: string) {}
      setAssistantmodeloptionsList(_: unknown[]) {}
    };
    setModel(_: unknown) {}
    setAssistantid(_: string) {}
    setDescription(_: string) {}
  }
  class CreateAssistantRequest {
    setAssistantprovider(_: unknown) {}
    setAssistanttoolsList(_: unknown[]) {}
    setName(_: string) {}
    setTagsList(_: string[]) {}
    setDescription(_: string) {}
  }
  class CreateAssistantToolRequest {
    setName(_: string) {}
    setDescription(_: string) {}
    setFields(_: unknown) {}
    setExecutionmethod(_: string) {}
    setExecutionoptionsList(_: unknown[]) {}
  }
  class GetAssistantRequest {
    setAssistantdefinition(_: unknown) {}
  }
  class AssistantDefinition {
    setAssistantid(_: string) {}
  }

  return {
    ConnectionConfig,
    Metadata,
    EndpointProviderModelAttribute,
    EndpointAttribute,
    CreateAssistantProviderRequest,
    CreateAssistantRequest,
    CreateAssistantToolRequest,
    GetAssistantRequest,
    AssistantDefinition,
    ForgotPassword: jest.fn(),
    CreateEndpoint: jest.fn(),
    GetEndpoint: jest.fn(),
    CreateEndpointProviderModel: jest.fn(),
    CreateAssistant: jest.fn(() => Promise.resolve({ getSuccess: () => false })),
    GetAssistant: jest.fn(() => Promise.resolve({ getSuccess: () => false })),
    CreateAssistantProvider: jest.fn(() => Promise.resolve({ getSuccess: () => false })),
  };
});

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useParams: () => mockParams,
  useNavigate: () => mockNavigate,
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

jest.mock('@/hooks/use-model', () => ({
  useAllProviderCredentials: () => ({ providerCredentials: [] }),
}));

jest.mock('@/hooks/use-global-navigator', () => ({
  useGlobalNavigation: () => ({
    goBack: jest.fn(),
    goToAssistant: jest.fn(),
    goToAssistantListing: jest.fn(),
    goToAssistantVersions: jest.fn(),
  }),
}));

jest.mock('@/app/pages/assistant/actions/hooks/use-confirmation', () => ({
  useConfirmDialog: () => ({
    showDialog: (cb: () => void) => cb(),
    ConfirmDialogComponent: () => null,
  }),
}));

jest.mock('@/app/components/form/tab-form', () => ({
  TabForm: ({ activeTab, errorMessage, form, formHeading }: any) => {
    const active = form.find((f: any) => f.code === activeTab) || form[0];
    return (
      <div>
        <h1>{formHeading}</h1>
        {errorMessage ? <div>{errorMessage}</div> : null}
        <div>{active.actions}</div>
      </div>
    );
  },
}));

jest.mock('@/app/components/providers/text', () => ({
  GetDefaultTextProviderConfigIfInvalid: () => [],
  ValidateTextProviderDefaultOptions: () => undefined,
  TextProvider: () => null,
}));

jest.mock('@/app/components/configuration/config-prompt', () => ({
  ConfigPrompt: () => null,
}));

jest.mock('@/app/components/tools', () => ({
  BuildinToolConfig: {},
}));

jest.mock('@/utils/prompt', () => ({
  ChatCompletePrompt: () => ({}),
  Prompt: () => ({ prompt: [], variables: [] }),
}));

jest.mock('@/utils', () => ({
  randomMeaningfullName: () => 'assistant-default',
  randomString: () => 'seed',
}));

jest.mock('@/app/components/error-container', () => ({
  ErrorContainer: ({ title, code }: any) => (
    <div>
      <span>{code}</span>
      <span>{title}</span>
    </div>
  ),
}));

jest.mock('@/app/components/helmet', () => ({ Helmet: () => null }));
jest.mock('@/app/components/form/button', () => ({
  IBlueBGArrowButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
  ICancelButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
}));

jest.mock('@/app/components/base/modal/confirm-ui', () => () => null);
jest.mock('@/app/components/base/modal/configure-endpoint-prompt-modal/index', () => ({
  ConfigureEndpointPromptDialog: () => null,
}));
jest.mock('@/app/components/base/modal/assistant-configure-next-modal', () => ({
  ConfigureAssistantNextDialog: () => null,
}));
jest.mock('@/app/components/base/modal/assistant-configure-tool-modal', () => ({
  ConfigureAssistantToolDialog: () => null,
}));
jest.mock('@/app/components/base/modal/configure-assistant-template-modal', () => ({
  ConfigureAssistantTemplateDialog: () => null,
}));

jest.mock('@/app/components/container/message/notice-block', () => ({
  YellowNoticeBlock: () => null,
}));
jest.mock('@/app/components/container/message/notice-block/doc-notice-block', () => ({
  DocNoticeBlock: ({ children }: any) => <div>{children}</div>,
}));

jest.mock('@/app/components/blocks/section-divider', () => ({
  SectionDivider: () => null,
}));
jest.mock('@/app/components/base/corner-border', () => ({
  CornerBorderOverlay: () => null,
}));

jest.mock('@/app/components/form/input', () => ({
  Input: require('react').forwardRef((props: any, ref: any) => <input ref={ref} {...props} />),
}));
jest.mock('@/app/components/form/error-message', () => ({
  ErrorMessage: ({ message }: any) => (message ? <div>{message}</div> : null),
}));
jest.mock('@/app/components/form/success-message', () => ({
  SuccessMessage: ({ message }: any) => (message ? <div>{message}</div> : null),
}));
jest.mock('@/app/components/form/fieldset', () => ({ FieldSet: ({ children }: any) => <div>{children}</div> }));
jest.mock('@/app/components/form-label', () => ({ FormLabel: ({ children }: any) => <label>{children}</label> }));
jest.mock('@/app/components/heading/action-heading/form-action-heading', () => ({
  FormActionHeading: ({ heading }: any) => <h2>{heading}</h2>,
}));

describe('Requested create/update flow pages', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockParams = {};
  });

  it('forgot password shows success message on success callback', async () => {
    (ForgotPassword as jest.Mock).mockImplementation((_cfg, _email, cb) => {
      cb(null, { getSuccess: () => true });
    });

    render(<ForgotPasswordPage />);
    fireEvent.change(screen.getByPlaceholderText('eg: john@rapida.ai'), {
      target: { value: 'user@x.com' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Send Email' }));

    expect(await screen.findByText(/Thanks! An email was sent/i)).toBeInTheDocument();
  });

  it('create endpoint blocks continue when prompt template variables are missing', () => {
    render(<CreateEndpointPage />);
    fireEvent.click(screen.getByRole('button', { name: 'Configure instruction' }));
    expect(
      screen.getByText(
        'Please provide a valid prompt template, it should at least have one variable.',
      ),
    ).toBeInTheDocument();
  });

  it('create endpoint version blocks continue when variables are missing', () => {
    render(<CreateNewVersionEndpointPage />);
    fireEvent.click(screen.getByRole('button', { name: 'Configure instruction' }));
    expect(screen.getByText('Please define at least one variable.')).toBeInTheDocument();
  });

  it('create assistant blocks continue when prompt content is empty', () => {
    render(<CreateAssistantPage />);
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    expect(
      screen.getByText('Each prompt message must have a valid role and non-empty content.'),
    ).toBeInTheDocument();
  });

  it('create assistant version shows unavailable state when assistantId is missing', () => {
    mockParams = {};
    render(<CreateVersionAssistantPage />);
    expect(screen.getByText('403')).toBeInTheDocument();
    expect(screen.getByText('Assistant not available')).toBeInTheDocument();
  });
});
