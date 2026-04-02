import { renderHook } from '@testing-library/react';

import { useGlobalNavigation } from '@/hooks/use-global-navigator';

const mockNavigate = jest.fn();

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useNavigate: () => mockNavigate,
}));

describe('useGlobalNavigation', () => {
  beforeEach(() => {
    mockNavigate.mockReset();
  });

  it('navigates backward and to direct routes', () => {
    const { result } = renderHook(() => useGlobalNavigation());

    result.current.goBack();
    result.current.goTo('/custom/path');
    result.current.goToDashboard();

    expect(mockNavigate).toHaveBeenNthCalledWith(1, -1);
    expect(mockNavigate).toHaveBeenNthCalledWith(2, '/custom/path');
    expect(mockNavigate).toHaveBeenNthCalledWith(3, '/dashboard');
  });

  it('builds assistant and deployment routes correctly', () => {
    const { result } = renderHook(() => useGlobalNavigation());

    result.current.goToAssistant('a-1');
    result.current.goToAssistantVersions('a-1');
    result.current.goToCreateAssistantVersion('a-1');
    result.current.goToConfigureApi('a-1');
    result.current.goToConfigureDebuggerExperience('a-1');
    result.current.goToConfigureDebuggerSTT('a-1');
    result.current.goToConfigureDebuggerTTS('a-1');
    result.current.goToCreateAssistantTool('a-1');

    expect(mockNavigate).toHaveBeenNthCalledWith(1, '/deployment/assistant/a-1');
    expect(mockNavigate).toHaveBeenNthCalledWith(
      2,
      '/deployment/assistant/a-1/version-history',
    );
    expect(mockNavigate).toHaveBeenNthCalledWith(
      3,
      '/deployment/assistant/a-1/create-new-version',
    );
    expect(mockNavigate).toHaveBeenNthCalledWith(
      4,
      '/deployment/assistant/a-1/deployment/api',
    );
    expect(mockNavigate).toHaveBeenNthCalledWith(
      5,
      '/deployment/assistant/a-1/deployment/debugger?editMode=section&section=experience',
    );
    expect(mockNavigate).toHaveBeenNthCalledWith(
      6,
      '/deployment/assistant/a-1/deployment/debugger?editMode=section&section=stt',
    );
    expect(mockNavigate).toHaveBeenNthCalledWith(
      7,
      '/deployment/assistant/a-1/deployment/debugger?editMode=section&section=tts',
    );
    expect(mockNavigate).toHaveBeenNthCalledWith(
      8,
      '/deployment/assistant/a-1/configure-tool/create',
    );
  });

  it('builds knowledge and integration routes correctly', () => {
    const { result } = renderHook(() => useGlobalNavigation());

    result.current.goToKnowledge('k-1');
    result.current.goToKnowledgeAddManualFile('k-1');
    result.current.goToKnowledgeAddCloudFile('k-1');
    result.current.goToKnowledgeAddStructureFile('k-1');
    result.current.goToModelInformation('openai');

    expect(mockNavigate).toHaveBeenNthCalledWith(1, '/knowledge/k-1');
    expect(mockNavigate).toHaveBeenNthCalledWith(
      2,
      '/knowledge/k-1/add-knowledge-file',
    );
    expect(mockNavigate).toHaveBeenNthCalledWith(3, '/knowledge/k-1/add-cloud-file');
    expect(mockNavigate).toHaveBeenNthCalledWith(
      4,
      '/knowledge/k-1/add-structure-file',
    );
    expect(mockNavigate).toHaveBeenNthCalledWith(5, '/integration/models/openai');
  });

  it('opens preview windows for chat and call routes', () => {
    const openSpy = jest
      .spyOn(window, 'open')
      .mockImplementation(() => null as unknown as Window);

    const { result } = renderHook(() => useGlobalNavigation());

    result.current.goToAssistantPreview('assistant-1');
    result.current.goToAssistantPreviewCall('assistant-1');

    expect(openSpy).toHaveBeenNthCalledWith(1, '/preview/chat/assistant-1');
    expect(openSpy).toHaveBeenNthCalledWith(2, '/preview/call/assistant-1');

    openSpy.mockRestore();
  });
});
