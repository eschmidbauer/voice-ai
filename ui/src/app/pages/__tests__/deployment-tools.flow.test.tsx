import React from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';

import { ConfigureAssistantDeploymentPage } from '@/app/pages/assistant/actions/create-deployment';
import { CreateTool } from '@/app/pages/assistant/actions/configure-assistant-tool/create-assistant-tool';
import { UpdateTool } from '@/app/pages/assistant/actions/configure-assistant-tool/update-assistant-tool';
import {
  CreateAssistantDebuggerDeployment,
  CreateAssistantTool,
  GetAssistant,
  GetAssistantDebuggerDeployment,
  GetAssistantApiDeployment,
  GetAssistantTool,
  UpdateAssistantTool,
} from '@rapidaai/react';

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
  goToConfigureDebuggerExperience: jest.fn(),
  goToConfigureDebuggerSTT: jest.fn(),
  goToConfigureDebuggerTTS: jest.fn(),
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
  class GetAssistantDeploymentRequest {
    setAssistantid(_: string) {}
  }
  class CreateAssistantDeploymentRequest {
    setDebugger(_: unknown) {}
    setApi(_: unknown) {}
    setPlugin(_: unknown) {}
    setPhone(_: unknown) {}
  }
  class AssistantDebuggerDeployment {
    setAssistantid(_: string) {}
    setGreeting(_: string) {}
    setMistake(_: string) {}
    setIdealtimeout(_: string) {}
    setIdealtimeoutbackoff(_: string) {}
    setIdealtimeoutmessage(_: string) {}
    setMaxsessionduration(_: string) {}
    setInputaudio(_: unknown) {}
    setOutputaudio(_: unknown) {}
  }
  class AssistantApiDeployment {
    setAssistantid(_: string) {}
    setGreeting(_: string) {}
    setMistake(_: string) {}
    setIdealtimeout(_: string) {}
    setIdealtimeoutbackoff(_: string) {}
    setIdealtimeoutmessage(_: string) {}
    setMaxsessionduration(_: string) {}
    setInputaudio(_: unknown) {}
    setOutputaudio(_: unknown) {}
  }
  class AssistantWebpluginDeployment {
    setAssistantid(_: string) {}
    setGreeting(_: string) {}
    setMistake(_: string) {}
    setIdealtimeout(_: string) {}
    setIdealtimeoutbackoff(_: string) {}
    setIdealtimeoutmessage(_: string) {}
    setMaxsessionduration(_: string) {}
    setInputaudio(_: unknown) {}
    setOutputaudio(_: unknown) {}
    setSuggestionList(_: unknown) {}
    setHelpcenterenabled(_: boolean) {}
    setProductcatalogenabled(_: boolean) {}
    setArticlecatalogenabled(_: boolean) {}
    setUploadfileenabled(_: boolean) {}
    getSuggestionList() { return []; }
  }
  class AssistantPhoneDeployment {
    setAssistantid(_: string) {}
    setGreeting(_: string) {}
    setMistake(_: string) {}
    setIdealtimeout(_: string) {}
    setIdealtimeoutbackoff(_: string) {}
    setIdealtimeoutmessage(_: string) {}
    setMaxsessionduration(_: string) {}
    setInputaudio(_: unknown) {}
    setOutputaudio(_: unknown) {}
    setPhoneprovidername(_: string) {}
    setPhoneoptionsList(_: unknown[]) {}
  }
  class DeploymentAudioProvider {
    setAudioprovider(_: string) {}
    setAudiooptionsList(_: unknown[]) {}
  }
  return {
    ConnectionConfig,
    Metadata,
    GetAssistantRequest,
    AssistantDefinition,
    AssistantDebuggerDeployment,
    AssistantApiDeployment,
    AssistantWebpluginDeployment,
    AssistantPhoneDeployment,
    DeploymentAudioProvider,
    GetAssistantDeploymentRequest,
    CreateAssistantDeploymentRequest,
    GetAssistant: jest.fn(),
    GetAssistantDebuggerDeployment: jest.fn(),
    GetAssistantApiDeployment: jest.fn(),
    GetAssistantWebpluginDeployment: jest.fn(),
    GetAssistantPhoneDeployment: jest.fn(),
    CreateAssistantDebuggerDeployment: jest.fn(),
    CreateAssistantApiDeployment: jest.fn(),
    CreateAssistantWebpluginDeployment: jest.fn(),
    CreateAssistantPhoneDeployment: jest.fn(),
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
        <div>{active.body}</div>
        <div>
          {Array.isArray(active.actions)
            ? active.actions.map((action: React.ReactElement, idx: number) => (
                <div key={idx}>{action}</div>
              ))
            : active.actions}
        </div>
      </div>
    );
  },
}));

jest.mock('@/app/components/tools', () => ({
  BuildinTool: ({ onChangeBuildinTool }: any) => (
    <div>
      <button onClick={() => onChangeBuildinTool('knowledge_retrieval')}>
        Use Knowledge Tool
      </button>
      <button onClick={() => onChangeBuildinTool('mcp')}>Use MCP Tool</button>
    </div>
  ),
  BuildinToolConfig: {},
  GetDefaultToolConfigIfInvalid: () => [],
  GetDefaultToolDefintion: (code: string, defaults: any) => {
    if (code === 'mcp') {
      return {
        name: 'mcp_tool',
        description: 'MCP tool',
        parameters: '{}',
      };
    }
    return defaults;
  },
  ValidateToolDefaultOptions: () => undefined,
}));

jest.mock('@/app/components/tools/common', () => ({
  ToolDefinitionForm: ({ toolDefinition, onChangeToolDefinition }: any) => (
    <div>
      <input
        aria-label="Tool Name"
        value={toolDefinition.name}
        onChange={e =>
          onChangeToolDefinition({ ...toolDefinition, name: e.target.value })
        }
      />
      <textarea
        aria-label="Tool Description"
        value={toolDefinition.description}
        onChange={e =>
          onChangeToolDefinition({
            ...toolDefinition,
            description: e.target.value,
          })
        }
      />
      <textarea
        aria-label="Tool Parameters"
        value={toolDefinition.parameters}
        onChange={e =>
          onChangeToolDefinition({
            ...toolDefinition,
            parameters: e.target.value,
          })
        }
      />
    </div>
  ),
}));

jest.mock('@/app/components/helmet', () => ({ Helmet: () => null }));
jest.mock('@/app/components/blocks/page-header-block', () => ({
  PageHeaderBlock: ({ children }: any) => <div>{children}</div>,
}));
jest.mock('@/app/components/blocks/page-title-block', () => ({
  PageTitleBlock: ({ children }: any) => <h2>{children}</h2>,
}));

jest.mock('@/app/components/carbon/button', () => ({
  PrimaryButton: ({ children, isLoading: _, renderIcon: _r, hasIconOnly: _h, iconDescription: _d, ...props }: any) => <button {...props}>{children}</button>,
  SecondaryButton: ({ children, isLoading: _, renderIcon: _r, hasIconOnly: _h, iconDescription: _d, ...props }: any) => <button {...props}>{children}</button>,
  GhostButton: ({ children, isLoading: _, renderIcon: _r, hasIconOnly: _h, iconDescription: _d, ...props }: any) => <button {...props}>{children}</button>,
  IconOnlyButton: ({ iconDescription, renderIcon: _r, tooltipPosition: _tp, ...props }: any) => (
    <button aria-label={iconDescription} {...props} />
  ),
}));
jest.mock('@/app/components/carbon/modal', () => ({
  Modal: ({ children, open }: any) => (open ? <div>{children}</div> : null),
  ModalHeader: ({ title }: any) => <div>{title}</div>,
  ModalBody: ({ children }: any) => <div>{children}</div>,
  ModalFooter: ({ children }: any) => <div>{children}</div>,
}));

jest.mock('@/hooks/use-model', () => ({
  useAllProviderCredentials: () => ({ providerCredentials: [] }),
}));

jest.mock('@/app/components/providers/telephony', () => ({
  TelephonyProvider: () => <div>telephony</div>,
  ValidateTelephonyOptions: () => true,
}));

jest.mock('@/app/components/base/modal/assistant-debugger-edit-section-modal/configure-experience-form', () => ({
  ConfigureExperienceModalForm: () => <div>experience-form</div>,
}));
jest.mock('@/app/components/base/modal/assistant-debugger-edit-section-modal/configure-web-experience-form', () => ({
  ConfigureWebExperienceModalForm: () => <div>web-experience-form</div>,
}));
jest.mock('@/app/components/base/modal/assistant-debugger-edit-section-modal/configure-audio-input-form', () => ({
  ConfigureAudioInputModalForm: () => <div>audio-input-form</div>,
}));
jest.mock('@/app/components/base/modal/assistant-debugger-edit-section-modal/configure-audio-output-form', () => ({
  ConfigureAudioOutputModalForm: () => <div>audio-output-form</div>,
}));

jest.mock('@/app/components/base/corner-border', () => ({
  CornerBorderOverlay: () => null,
}));

jest.mock('@/app/components/providers/speech-to-text/provider', () => ({
  GetDefaultMicrophoneConfig: () => [],
  GetDefaultSpeechToTextIfInvalid: () => [],
  ValidateSpeechToTextIfInvalid: () => undefined,
}));
jest.mock('@/app/components/providers/text-to-speech/provider', () => ({
  GetDefaultSpeakerConfig: () => [],
  GetDefaultTextToSpeechIfInvalid: () => [],
  ValidateTextToSpeechIfInvalid: () => undefined,
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

jest.mock('@/app/components/carbon/empty-state', () => ({
  EmptyState: ({ title, subtitle, action, onAction, actionComponent }: any) => (
    <div>
      <div>{title}</div>
      <div>{subtitle}</div>
      {actionComponent}
      {action ? <button onClick={onAction}>{action}</button> : null}
    </div>
  ),
}));

jest.mock('@/app/components/input-helper', () => ({ InputHelper: () => null }));
jest.mock('@/app/components/form-label', () => ({ FormLabel: ({ children }: any) => <label>{children}</label> }));
jest.mock('@/app/components/form/fieldset', () => ({ FieldSet: ({ children }: any) => <div>{children}</div> }));
jest.mock('@/app/components/carbon/button/copy-button', () => ({ CopyButton: () => null }));

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

    (GetAssistantDebuggerDeployment as jest.Mock).mockResolvedValue({
      getData: () => ({
        getGreeting: () => 'hello',
        getMistake: () => '',
        getIdealtimeout: () => '30',
        getIdealtimeoutmessage: () => 'Are you there?',
        getMaxsessionduration: () => '300',
        getIdealtimeoutbackoff: () => '2',
        getInputaudio: () => ({
          getAudioprovider: () => 'deepgram',
          getAudiooptionsList: () => [],
        }),
        getOutputaudio: () => ({
          getAudioprovider: () => 'cartesia',
          getAudiooptionsList: () => [],
        }),
      }),
    });
    (CreateAssistantDebuggerDeployment as jest.Mock).mockResolvedValue({
      getSuccess: () => true,
      getData: () => ({ id: 'dep-1' }),
    });

    (CreateAssistantTool as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _name, _desc, _fields, _method, _opts, cb) =>
        cb(null, {
          getSuccess: () => true,
          getData: () => ({ getName: () => _name }),
        }),
    );

    (UpdateAssistantTool as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _toolId, _name, _desc, _fields, _method, _opts, cb) =>
        cb(null, {
          getSuccess: () => true,
        }),
    );
  });

  it('create deployment allows channel selection and routes to web deployment', async () => {
    render(<ConfigureAssistantDeploymentPage />);

    fireEvent.click(screen.getAllByRole('button', { name: /Add deployment/i })[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: /Web Widget/i }));

    expect(mockGlobalNavigation.goToConfigureWeb).toHaveBeenCalledWith('assistant-1');
    await act(async () => {});
  });

  it('create deployment empty state shows add deployment action', async () => {
    (GetAssistant as jest.Mock).mockResolvedValue({
      getSuccess: () => true,
      getData: () => ({
        getApideployment: () => null,
        hasApideployment: () => false,
        getWebplugindeployment: () => null,
        hasWebplugindeployment: () => false,
        getDebuggerdeployment: () => null,
        hasDebuggerdeployment: () => false,
        getPhonedeployment: () => null,
        hasPhonedeployment: () => false,
      }),
    });

    render(<ConfigureAssistantDeploymentPage />);

    await waitFor(() => {
      expect(screen.getByText('No deployments yet')).toBeInTheDocument();
    });

    fireEvent.click(
      screen.getAllByRole('button', { name: /Add deployment/i })[1],
    );
    fireEvent.click(screen.getByRole('menuitem', { name: /Debugger/i }));
    expect(mockGlobalNavigation.goToConfigureDebugger).toHaveBeenCalledWith(
      'assistant-1',
    );
    await act(async () => {});
  });

  it('create deployment routes to API, phone and debugger channels from add deployment menu', async () => {
    render(<ConfigureAssistantDeploymentPage />);

    fireEvent.click(screen.getAllByRole('button', { name: /Add deployment/i })[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: /SDK \/ API/i }));
    expect(mockGlobalNavigation.goToConfigureApi).toHaveBeenCalledWith(
      'assistant-1',
    );

    fireEvent.click(screen.getAllByRole('button', { name: /Add deployment/i })[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: /Phone Call/i }));
    expect(mockGlobalNavigation.goToConfigureCall).toHaveBeenCalledWith(
      'assistant-1',
    );

    fireEvent.click(screen.getAllByRole('button', { name: /Add deployment/i })[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: /Debugger/i }));
    expect(mockGlobalNavigation.goToConfigureDebugger).toHaveBeenCalledWith(
      'assistant-1',
    );
    await act(async () => {});
  });

  it('create deployment shows edit action for existing API deployment', async () => {
    (GetAssistantApiDeployment as jest.Mock).mockResolvedValue({
      getData: () => ({
        getGreeting: () => '',
        getMistake: () => '',
        getIdealtimeout: () => '30',
        getIdealtimeoutmessage: () => '',
        getMaxsessionduration: () => '300',
        getIdealtimeoutbackoff: () => '2',
        getInputaudio: () => null,
        getOutputaudio: () => null,
      }),
    });

    render(<ConfigureAssistantDeploymentPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Experience' }));
    await waitFor(() =>
      expect(screen.getByText('General Experience')).toBeInTheDocument(),
    );
    await act(async () => {});
  });

  it('debugger edit menu opens section modals', async () => {
    (GetAssistant as jest.Mock).mockResolvedValue({
      getSuccess: () => true,
      getData: () => ({
        getApideployment: () => null,
        hasApideployment: () => false,
        getWebplugindeployment: () => null,
        hasWebplugindeployment: () => false,
        getDebuggerdeployment: () => ({
          getInputaudio: () => true,
          getOutputaudio: () => true,
          getCreateddate: () => '2026-01-01',
        }),
        hasDebuggerdeployment: () => true,
        getPhonedeployment: () => null,
        hasPhonedeployment: () => false,
      }),
    });

    render(<ConfigureAssistantDeploymentPage />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(document.querySelector('.cds--menu-button__bottom-end')).toBeTruthy();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Experience' }));
    await waitFor(() =>
      expect(screen.getByText('General Experience')).toBeInTheDocument(),
    );
    expect(mockGlobalNavigation.goToConfigureDebuggerExperience).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Voice Input' }));
    await waitFor(() =>
      expect(screen.getByText('Voice Input')).toBeInTheDocument(),
    );
    expect(mockGlobalNavigation.goToConfigureDebuggerSTT).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Voice Output' }));
    await waitFor(() =>
      expect(screen.getByText('Voice Output')).toBeInTheDocument(),
    );
    expect(mockGlobalNavigation.goToConfigureDebuggerTTS).not.toHaveBeenCalled();
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

  it('create tool validates invalid JSON parameters on definition step', () => {
    render(<CreateTool assistantId="assistant-1" />);

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.change(screen.getByLabelText('Tool Name'), {
      target: { value: 'valid_tool' },
    });
    fireEvent.change(screen.getByLabelText('Tool Parameters'), {
      target: { value: '{not-json' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Configure Tool' }));

    expect(
      screen.getByText('Please provide valid parameters, must be a valid JSON.'),
    ).toBeInTheDocument();
  });

  it('update tool validates missing description on definition step', () => {
    (GetAssistantTool as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _toolId, cb) =>
        cb(null, {
          getData: () => ({
            getName: () => 'existing_tool',
            getDescription: () => 'existing description',
            getFields: () => ({ toJavaScript: () => ({ context: 'ok' }) }),
            getExecutionmethod: () => 'knowledge_retrieval',
            getExecutionoptionsList: () => [],
          }),
        }),
    );

    render(<UpdateTool assistantId="assistant-1" />);

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    fireEvent.change(screen.getByLabelText('Tool Description'), {
      target: { value: '' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Update Tool' }));

    expect(
      screen.getByText('Please provide a description for the tool.'),
    ).toBeInTheDocument();
  });

  it('create tool submits directly for MCP from action step', () => {
    render(<CreateTool assistantId="assistant-1" />);

    fireEvent.click(screen.getByRole('button', { name: 'Use MCP Tool' }));
    fireEvent.click(screen.getByRole('button', { name: 'Configure Tool' }));

    expect(CreateAssistantTool).toHaveBeenCalledTimes(1);
  });
});
