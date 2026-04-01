import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import {
  ConfigureAssistantDebuggerDeploymentPage,
} from '@/app/pages/assistant/actions/create-deployment/debugger';
import {
  CreateAssistantDebuggerDeployment,
  GetAssistantDebuggerDeployment,
} from '@rapidaai/react';

let mockParams: Record<string, string | undefined> = {
  assistantId: 'assistant-1',
};

jest.mock('@rapidaai/react', () => {
  class ConnectionConfig {
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

  class DeploymentAudioProvider {
    private audioProvider = '';
    private audioOptions: Metadata[] = [];
    setAudioprovider(v: string) {
      this.audioProvider = v;
    }
    getAudioprovider() {
      return this.audioProvider;
    }
    setAudiooptionsList(v: Metadata[]) {
      this.audioOptions = v;
    }
    getAudiooptionsList() {
      return this.audioOptions;
    }
  }

  class AssistantDebuggerDeployment {
    private inputAudio?: DeploymentAudioProvider;
    private outputAudio?: DeploymentAudioProvider;
    setAssistantid(_: string) {}
    setGreeting(_: string) {}
    setMistake(_: string) {}
    setIdealtimeout(_: string) {}
    setIdealtimeoutbackoff(_: string) {}
    setIdealtimeoutmessage(_: string) {}
    setMaxsessionduration(_: string) {}
    setInputaudio(v: DeploymentAudioProvider) {
      this.inputAudio = v;
    }
    getInputaudio() {
      return this.inputAudio;
    }
    setOutputaudio(v: DeploymentAudioProvider) {
      this.outputAudio = v;
    }
    getOutputaudio() {
      return this.outputAudio;
    }
  }

  class CreateAssistantDeploymentRequest {
    private debuggerDeployment?: AssistantDebuggerDeployment;
    setDebugger(v: AssistantDebuggerDeployment) {
      this.debuggerDeployment = v;
    }
    getDebugger() {
      return this.debuggerDeployment;
    }
  }

  class GetAssistantDeploymentRequest {
    setAssistantid(_: string) {}
  }

  return {
    ConnectionConfig,
    Metadata,
    DeploymentAudioProvider,
    AssistantDebuggerDeployment,
    CreateAssistantDeploymentRequest,
    GetAssistantDeploymentRequest,
    GetAssistantDebuggerDeployment: jest.fn(),
    CreateAssistantDebuggerDeployment: jest.fn(),
  };
});

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useParams: () => mockParams,
}));

jest.mock('@/hooks', () => ({
  useRapidaStore: () => ({
    loading: false,
    showLoader: jest.fn(),
    hideLoader: jest.fn(),
  }),
}));

jest.mock('@/hooks/use-model', () => ({
  useAllProviderCredentials: () => ({ providerCredentials: [] }),
}));

jest.mock('@/hooks/use-credential', () => ({
  useCurrentCredential: () => ({ authId: 'u-1', projectId: 'p-1', token: 't-1' }),
}));

jest.mock('@/hooks/use-global-navigator', () => ({
  useGlobalNavigation: () => ({
    goToDeploymentAssistant: jest.fn(),
  }),
}));

jest.mock('@/app/components/helmet', () => ({ Helmet: () => null }));
jest.mock('@/app/components/base/modal/debugger-deployment-success-modal', () => ({
  DebuggerDeploymentSuccessDialog: () => null,
}));
jest.mock('@/app/components/base/cards', () => ({
  BaseCard: ({ children }: any) => <div>{children}</div>,
}));
jest.mock('@/app/components/form/checkbox', () => ({
  InputCheckbox: ({ children, ...props }: any) => (
    <label>
      <input type="checkbox" {...props} />
      {children}
    </label>
  ),
}));
jest.mock('@/app/components/input-helper', () => ({
  InputHelper: ({ children }: any) => <div>{children}</div>,
}));
jest.mock('@/app/components/form/switch', () => ({
  SwitchWithLabel: ({ enable, setEnable, label }: any) => (
    <button type="button" onClick={() => setEnable(!enable)}>
      {label}
    </button>
  ),
}));

jest.mock('@/app/components/form/tab-form', () => ({
  TabForm: ({ activeTab, form, errorMessage }: any) => {
    const active = form.find((x: any) => x.code === activeTab);
    return (
      <div>
        {errorMessage ? <div>{errorMessage}</div> : null}
        <div>{active?.body}</div>
        <div>
          {active?.actions?.map((action: React.ReactElement, idx: number) => (
            <div key={idx}>{action}</div>
          ))}
        </div>
      </div>
    );
  },
}));

jest.mock('@/app/pages/assistant/actions/create-deployment/commons/configure-experience', () => ({
  ConfigureExperience: () => <div>experience</div>,
}));

jest.mock('@/app/pages/assistant/actions/create-deployment/commons/configure-audio-input', () => ({
  ConfigureAudioInputProvider: () => <div>audio-input</div>,
}));

jest.mock('@/app/pages/assistant/actions/create-deployment/commons/configure-audio-output', () => ({
  ConfigureAudioOutputProvider: () => <div>audio-output</div>,
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

jest.mock('@/app/pages/assistant/actions/hooks/use-confirmation', () => {
  const React = require('react');
  return {
    useConfirmDialog: ({ title = 'Are you sure?' }: { title?: string } = {}) => {
      const [isOpen, setIsOpen] = React.useState(false);
      const [onConfirm, setOnConfirm] = React.useState<() => void>(() => () => {});

      return {
        showDialog: (cb: () => void) => {
          setOnConfirm(() => cb);
          setIsOpen(true);
        },
        ConfirmDialogComponent: () =>
          isOpen ? (
            <button
              onClick={() => {
                onConfirm();
                setIsOpen(false);
              }}
            >
              {title}
            </button>
          ) : null,
      };
    },
  };
});

jest.mock('@/app/components/carbon/button', () => ({
  PrimaryButton: ({ children, isLoading, ...props }: any) => <button {...props}>{children}</button>,
  GhostButton: ({ children, isLoading, ...props }: any) => <button {...props}>{children}</button>,
  SecondaryButton: ({ children, isLoading, ...props }: any) => <button {...props}>{children}</button>,
}));

describe('Debugger deployment voice input intent actions', () => {
  const mockEditDeployment = () => ({
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

  beforeEach(() => {
    jest.clearAllMocks();

    (GetAssistantDebuggerDeployment as jest.Mock).mockResolvedValue(
      mockEditDeployment(),
    );

    (CreateAssistantDebuggerDeployment as jest.Mock).mockResolvedValue({
      getData: () => ({ id: 'dep-1' }),
      getSuccess: () => true,
    });
  });

  it('Continue keeps existing voice input enabled', async () => {
    render(<ConfigureAssistantDebuggerDeploymentPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await waitFor(() =>
      expect(screen.getByText(/Voice input is currently/i)).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.click(screen.getByRole('button', { name: 'Deploy Debugger' }));

    await waitFor(() =>
      expect(CreateAssistantDebuggerDeployment).toHaveBeenCalledTimes(1),
    );

    const req = (CreateAssistantDebuggerDeployment as jest.Mock).mock.calls[0][1];
    const deployment = req.getDebugger();
    expect(deployment.getInputaudio()).toBeDefined();
  });

  it('unchecking voice input removes input audio on save', async () => {
    render(<ConfigureAssistantDebuggerDeploymentPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await waitFor(() =>
      expect(screen.getByText(/Voice input is currently/i)).toBeInTheDocument(),
    );
    fireEvent.click(
      screen.getByLabelText('Enable voice input (Speech-to-Text)'),
    );
    expect(
      screen.getByText(/Voice input is disabled\./i),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.click(screen.getByRole('button', { name: 'Deploy Debugger' }));

    await waitFor(() =>
      expect(CreateAssistantDebuggerDeployment).toHaveBeenCalledTimes(1),
    );

    const req = (CreateAssistantDebuggerDeployment as jest.Mock).mock.calls[0][1];
    const deployment = req.getDebugger();
    expect(deployment.getInputaudio()).toBeUndefined();
  });

  it('unchecking voice output removes output audio on save', async () => {
    render(<ConfigureAssistantDebuggerDeploymentPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await waitFor(() =>
      expect(screen.getByText(/Voice output is currently/i)).toBeInTheDocument(),
    );
    fireEvent.click(
      screen.getByLabelText('Enable voice output (Text-to-Speech)'),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Deploy Debugger' }));

    await waitFor(() =>
      expect(CreateAssistantDebuggerDeployment).toHaveBeenCalledTimes(1),
    );

    const req = (CreateAssistantDebuggerDeployment as jest.Mock).mock.calls[0][1];
    const deployment = req.getDebugger();
    expect(deployment.getOutputaudio()).toBeUndefined();
  });

  it('create mode deploys without existing deployment data', async () => {
    (GetAssistantDebuggerDeployment as jest.Mock).mockResolvedValue({
      getData: () => null,
    });

    render(<ConfigureAssistantDebuggerDeploymentPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.click(screen.getByRole('button', { name: 'Deploy Debugger' }));

    await waitFor(() =>
      expect(CreateAssistantDebuggerDeployment).toHaveBeenCalledTimes(1),
    );

    const req = (CreateAssistantDebuggerDeployment as jest.Mock).mock.calls[0][1];
    const deployment = req.getDebugger();
    expect(deployment.getInputaudio()).toBeDefined();
    expect(deployment.getOutputaudio()).toBeDefined();
  });
});
