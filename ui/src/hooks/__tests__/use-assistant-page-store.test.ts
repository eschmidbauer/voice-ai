import {
  CreateAssistantTag,
  GetAllAssistant,
  GetAssistant,
  UpdateAssistantDetail,
} from '@rapidaai/react';

import { useAssistantPageStore } from '@/hooks/use-assistant-page-store';

jest.mock('@rapidaai/react', () => {
  class ConnectionConfig {
    constructor(_: unknown) {}

    static WithDebugger(config: unknown) {
      return config;
    }
  }

  class AssistantDefinition {
    private assistantId = '';
    private version = '';

    setAssistantid(id: string) {
      this.assistantId = id;
    }

    setVersion(version: string) {
      this.version = version;
    }

    getAssistantid() {
      return this.assistantId;
    }

    getVersion() {
      return this.version;
    }
  }

  class GetAssistantRequest {
    private assistantDefinition: AssistantDefinition | null = null;

    setAssistantdefinition(def: AssistantDefinition) {
      this.assistantDefinition = def;
    }

    getAssistantdefinition() {
      return this.assistantDefinition;
    }
  }

  return {
    ConnectionConfig,
    AssistantDefinition,
    GetAssistantRequest,
    GetAllAssistant: jest.fn(),
    GetAssistant: jest.fn(),
    UpdateAssistantDetail: jest.fn(),
    CreateAssistantTag: jest.fn(),
  };
});

const resetStore = () => {
  useAssistantPageStore.setState({
    currentAssistant: null,
    assistants: [],
    instructionVisible: false,
    editTagVisible: false,
    updateDescriptionVisible: false,
    page: 1,
    pageSize: 20,
    totalCount: 0,
    criteria: [],
    columns: [],
  } as any);
};

const makeAssistant = (id: string) => ({ getId: () => id } as any);

describe('useAssistantPageStore', () => {
  beforeEach(() => {
    resetStore();
    (GetAllAssistant as jest.Mock).mockReset();
    (GetAssistant as jest.Mock).mockReset();
    (UpdateAssistantDetail as jest.Mock).mockReset();
    (CreateAssistantTag as jest.Mock).mockReset();
  });

  it('adds and merges criteria', () => {
    const state = useAssistantPageStore.getState();

    state.addCriteria('status', 'active', 'and');
    state.addCriteria('status', 'paused', 'and');
    state.addCriterias([
      { k: 'status', v: 'ready', logic: 'and' },
      { k: 'owner', v: 'u-1', logic: 'or' },
    ]);

    expect(useAssistantPageStore.getState().criteria).toEqual([
      { key: 'status', value: 'ready', logic: 'and' },
      { key: 'owner', value: 'u-1', logic: 'or' },
    ]);
  });

  it('handles successful onGetAllAssistant response', () => {
    const assistant = makeAssistant('a-1');
    const onSuccess = jest.fn();
    const onError = jest.fn();

    (GetAllAssistant as jest.Mock).mockImplementation(
      (_cfg, _page, _pageSize, _criteria, callback) => {
        callback(null, {
          getSuccess: () => true,
          getDataList: () => [assistant],
          getPaginated: () => ({ getTotalitem: () => 7 }),
        });
      },
    );

    useAssistantPageStore
      .getState()
      .onGetAllAssistant('project-1', 'token-1', 'user-1', onError, onSuccess);

    expect(onSuccess).toHaveBeenCalledWith([assistant]);
    expect(onError).not.toHaveBeenCalled();
    expect(useAssistantPageStore.getState().assistants).toEqual([assistant]);
    expect(useAssistantPageStore.getState().totalCount).toBe(7);
  });

  it('uses error message from onGetAllAssistant response', () => {
    const onSuccess = jest.fn();
    const onError = jest.fn();

    (GetAllAssistant as jest.Mock).mockImplementation(
      (_cfg, _page, _pageSize, _criteria, callback) => {
        callback(null, {
          getSuccess: () => false,
          getError: () => ({ getHumanmessage: () => 'assistant list failed' }),
        });
      },
    );

    useAssistantPageStore
      .getState()
      .onGetAllAssistant('project-1', 'token-1', 'user-1', onError, onSuccess);

    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith('assistant list failed');
  });

  it('uses fallback error when onGetAllAssistant has no error object', () => {
    const onError = jest.fn();

    (GetAllAssistant as jest.Mock).mockImplementation(
      (_cfg, _page, _pageSize, _criteria, callback) => {
        callback(null, {
          getSuccess: () => false,
          getError: () => null,
        });
      },
    );

    useAssistantPageStore
      .getState()
      .onGetAllAssistant('project-1', 'token-1', 'user-1', onError, jest.fn());

    expect(onError).toHaveBeenCalledWith(
      'Something went wrong while retrieving your assistants. Please refresh the page or try again later.',
    );
  });

  it('handles successful onGetAssistant response', async () => {
    const assistant = makeAssistant('a-2');
    const onSuccess = jest.fn();
    const onError = jest.fn();

    (GetAssistant as jest.Mock).mockResolvedValue({
      getSuccess: () => true,
      getData: () => assistant,
    });

    useAssistantPageStore
      .getState()
      .onGetAssistant(
        'assistant-1',
        'v-1',
        'project-1',
        'token-1',
        'user-1',
        onSuccess,
        onError,
      );

    await Promise.resolve();

    expect(onSuccess).toHaveBeenCalledWith(assistant);
    expect(onError).not.toHaveBeenCalled();
  });

  it('uses API error message from onGetAssistant response', async () => {
    const onSuccess = jest.fn();
    const onError = jest.fn();

    (GetAssistant as jest.Mock).mockResolvedValue({
      getSuccess: () => false,
      getError: () => ({ getHumanmessage: () => 'assistant detail failed' }),
    });

    useAssistantPageStore
      .getState()
      .onGetAssistant(
        'assistant-1',
        null,
        'project-1',
        'token-1',
        'user-1',
        onSuccess,
        onError,
      );

    await Promise.resolve();

    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith('assistant detail failed');
  });

  it('uses fallback error when onGetAssistant promise rejects', async () => {
    const onSuccess = jest.fn();
    const onError = jest.fn();

    (GetAssistant as jest.Mock).mockRejectedValue(new Error('network down'));

    useAssistantPageStore
      .getState()
      .onGetAssistant(
        'assistant-1',
        null,
        'project-1',
        'token-1',
        'user-1',
        onSuccess,
        onError,
      );

    await Promise.resolve();
    await Promise.resolve();

    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith(
      'Something went wrong while retrieving your assistant. Please refresh the page or try again later.',
    );
  });

  it('updates description and reloads assistant on success', () => {
    const updated = makeAssistant('a-3');
    const onSuccess = jest.fn();
    const onError = jest.fn();

    (UpdateAssistantDetail as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _name, _desc, callback) => {
        callback(null, {
          getSuccess: () => true,
          getData: () => updated,
        });
      },
    );

    useAssistantPageStore
      .getState()
      .onUpdateAssistantDescription(
        'assistant-1',
        'new-name',
        'new-description',
        'project-1',
        'token-1',
        'user-1',
        onError,
        onSuccess,
      );

    expect(onSuccess).toHaveBeenCalledWith(updated);
    expect(onError).not.toHaveBeenCalled();
    expect(useAssistantPageStore.getState().currentAssistant).toBe(updated);
  });

  it('uses fallback error when update description fails without error object', () => {
    const onSuccess = jest.fn();
    const onError = jest.fn();

    (UpdateAssistantDetail as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _name, _desc, callback) => {
        callback(null, {
          getSuccess: () => false,
          getError: () => null,
        });
      },
    );

    useAssistantPageStore
      .getState()
      .onUpdateAssistantDescription(
        'assistant-1',
        'new-name',
        'new-description',
        'project-1',
        'token-1',
        'user-1',
        onError,
        onSuccess,
      );

    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith(
      'Unable to update assistant, please try again later.',
    );
  });

  it('updates assistant tag and returns updated assistant', () => {
    const updated = makeAssistant('a-4');
    const onSuccess = jest.fn();
    const onError = jest.fn();

    (CreateAssistantTag as jest.Mock).mockImplementation(
      (_cfg, _assistantId, _tags, callback) => {
        callback(null, {
          getSuccess: () => true,
          getData: () => updated,
        });
      },
    );

    useAssistantPageStore
      .getState()
      .onCreateAssistantTag(
        'assistant-1',
        ['prod'],
        'project-1',
        'token-1',
        'user-1',
        onError,
        onSuccess,
      );

    expect(onSuccess).toHaveBeenCalledWith(updated);
    expect(onError).not.toHaveBeenCalled();
  });

  it('resets state using clear', () => {
    useAssistantPageStore.setState({
      currentAssistant: makeAssistant('a-9'),
      assistants: [makeAssistant('a-9')],
      instructionVisible: true,
      editTagVisible: true,
      updateDescriptionVisible: true,
      page: 4,
      pageSize: 50,
      totalCount: 999,
      criteria: [{ key: 'status', value: 'active', logic: 'and' }],
    } as any);

    useAssistantPageStore.getState().clear();

    expect(useAssistantPageStore.getState().currentAssistant).toBeNull();
    expect(useAssistantPageStore.getState().assistants).toEqual([]);
    expect(useAssistantPageStore.getState().instructionVisible).toBe(false);
    expect(useAssistantPageStore.getState().editTagVisible).toBe(false);
    expect(useAssistantPageStore.getState().updateDescriptionVisible).toBe(false);
    expect(useAssistantPageStore.getState().page).toBe(1);
    expect(useAssistantPageStore.getState().pageSize).toBe(20);
    expect(useAssistantPageStore.getState().totalCount).toBe(0);
    expect(useAssistantPageStore.getState().criteria).toEqual([]);
  });
});
