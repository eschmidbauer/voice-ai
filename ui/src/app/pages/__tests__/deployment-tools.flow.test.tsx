import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';

import { ConfigureAssistantDeploymentPage } from '@/app/pages/assistant/actions/create-deployment';
import { CreateTool } from '@/app/pages/assistant/actions/configure-assistant-tool/create-assistant-tool';
import { UpdateTool } from '@/app/pages/assistant/actions/configure-assistant-tool/update-assistant-tool';
import { GetAssistant, GetAssistantTool } from '@rapidaai/react';

let mockParams: Record<string, string | undefined> = {
  assistantId: 'assistant-1',
  assistantToolId: 'tool-1',
};
const mockNavigate = jest.fn();
const mockShowLoader = jest.fn();
const mockHideLoader = jest.fn();

const mockGlobalNavigation = {
  goBack: jest.fn(),
  goToConfigureWeb: jest.fn(),
  goToConfigureApi: jest.fn(),
  goToConfigureCall: jest.fn(),
  goToConfigureDebugger: jest.fn(),
  goToConfigureAssistantTool: jest.fn(),
};

jest.mock('@rapidaai/react', () => {
  class ConnectionConfig {
    constructor(_: unknown) {}
    static WithDebugger(config: unknown) {
      return config;
    }
  }
  class GetAssistantRequest {
    setAssistantdefinition(_: unknown) {}
  }
  class AssistantDefinition {
    setAssistantid(_: string) {}
  }
  return {
    ConnectionConfig,
    GetAssistantRequest,
    AssistantDefinition,
    GetAssistant: jest.fn(),
    GetAssistantTool: jest.fn(),
    CreateAssistantTool: jest.fn(),
    UpdateAssistantTool: jest.fn(),
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

jest.mock('@/hooks/use-global-navigator', () => ({
  useGlobalNavigation: () => mockGlobalNavigation,
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

jest.mock('@/app/components/tools', () => ({
  BuildinTool: () => null,
  BuildinToolConfig: {},
  GetDefaultToolConfigIfInvalid: () => [],
  GetDefaultToolDefintion: (_code: string, defaults: any) => defaults,
  ValidateToolDefaultOptions: () => undefined,
}));

jest.mock('@/app/components/tools/common', () => ({
  ToolDefinitionForm: () => null,
}));

jest.mock('@/app/components/helmet', () => ({ Helmet: () => null }));
jest.mock('@/app/components/blocks/page-header-block', () => ({
  PageHeaderBlock: ({ children }: any) => <div>{children}</div>,
}));
jest.mock('@/app/components/blocks/page-title-block', () => ({
  PageTitleBlock: ({ children }: any) => <h2>{children}</h2>,
}));

jest.mock('@/app/components/form/button', () => ({
  IBlueBGButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
  IButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
  IBlueBGArrowButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
  ICancelButton: ({ children, ...props }: any) => <button {...props}>{children}</button>,
}));

jest.mock('@/app/components/popover', () => ({
  Popover: ({ children, open }: any) => (open ? <div>{children}</div> : null),
}));

jest.mock('@/app/components/base/cards', () => ({
  BaseCard: ({ children }: any) => <div>{children}</div>,
}));

jest.mock('@/app/components/base/modal/assistant-phone-call-deployment-modal', () => ({
  AssistantPhoneCallDeploymentDialog: () => null,
}));
jest.mock('@/app/components/base/modal/assistant-debug-deployment-modal', () => ({
  AssistantDebugDeploymentDialog: () => null,
}));
jest.mock('@/app/components/base/modal/assistant-web-widget-deployment-modal', () => ({
  AssistantWebWidgetlDeploymentDialog: () => null,
}));
jest.mock('@/app/components/base/modal/assistant-api-deployment-modal', () => ({
  AssistantApiDeploymentDialog: () => null,
}));

jest.mock('@/app/components/container/message/actionable-empty-message', () => ({
  ActionableEmptyMessage: ({ action, onActionClick }: any) => (
    <button onClick={onActionClick}>{action}</button>
  ),
}));

jest.mock('@/app/components/indicators/revision', () => ({
  RevisionIndicator: () => null,
}));

jest.mock('@/app/components/input-helper', () => ({ InputHelper: () => null }));
jest.mock('@/app/components/form-label', () => ({ FormLabel: ({ children }: any) => <label>{children}</label> }));
jest.mock('@/app/components/form/fieldset', () => ({ FieldSet: ({ children }: any) => <div>{children}</div> }));
jest.mock('@/app/components/form/button/copy-button', () => ({ CopyButton: () => null }));
jest.mock('@/app/components/Icon/plus', () => ({ PlusIcon: () => <span>+</span> }));

jest.mock('@/utils/date', () => ({
  toHumanReadableDateTime: () => 'date-time',
}));

jest.mock('@/utils', () => ({
  cn: (...args: string[]) => args.filter(Boolean).join(' '),
}));

describe('Deployment and tool flows', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockParams = { assistantId: 'assistant-1', assistantToolId: 'tool-1' };

    (GetAssistant as jest.Mock).mockResolvedValue({
      getSuccess: () => true,
      getData: () => ({
        getApideployment: () => ({
          getInputaudio: () => false,
          getOutputaudio: () => false,
          getCreateddate: () => '2026-01-01',
        }),
        hasApideployment: () => true,
        getWebplugindeployment: () => null,
        hasWebplugindeployment: () => false,
        getDebuggerdeployment: () => null,
        hasDebuggerdeployment: () => false,
        getPhonedeployment: () => null,
        hasPhonedeployment: () => false,
      }),
    });

    (GetAssistantTool as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _toolId, cb) => cb(null, { getData: () => null }),
    );
  });

  it('create deployment allows channel selection and routes to web deployment', async () => {
    render(<ConfigureAssistantDeploymentPage />);

    fireEvent.click(screen.getAllByRole('button', { name: /Add deployment/i })[0]);
    fireEvent.click(screen.getByRole('button', { name: /Web Widget/i }));

    expect(mockGlobalNavigation.goToConfigureWeb).toHaveBeenCalledWith('assistant-1');
  });

  it('create deployment shows edit action for existing API deployment', async () => {
    render(<ConfigureAssistantDeploymentPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(mockGlobalNavigation.goToConfigureApi).toHaveBeenCalledWith('assistant-1');
  });

  it('create tool validates missing name before submit', () => {
    render(<CreateTool assistantId="assistant-1" />);

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.click(screen.getByRole('button', { name: 'Configure Tool' }));

    expect(screen.getByText('Please provide a valid name for tool.')).toBeInTheDocument();
  });

  it('update tool validates missing name before submit', () => {
    render(<UpdateTool assistantId="assistant-1" />);

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.click(screen.getByRole('button', { name: 'Update Tool' }));

    expect(screen.getByText('Please provide a valid name for tool.')).toBeInTheDocument();
  });
});
