import {
  GetAllEndpoint,
  GetEndpoint,
  CreateEndpointTag,
  CreateEndpointRetryConfiguration,
  CreateEndpointCacheConfiguration,
  UpdateEndpointDetail,
} from '@rapidaai/react';

import {
  initialEndpointType,
  useEndpointPageStore,
} from '@/hooks/use-endpoint-page-store';

jest.mock('@rapidaai/react', () => {
  class ConnectionConfig {
    constructor(_: unknown) {}

    static WithDebugger(config: unknown) {
      return config;
    }
  }

  return {
    ConnectionConfig,
    GetAllEndpoint: jest.fn(),
    GetEndpoint: jest.fn(),
    CreateEndpointTag: jest.fn(),
    CreateEndpointRetryConfiguration: jest.fn(),
    CreateEndpointCacheConfiguration: jest.fn(),
    UpdateEndpointDetail: jest.fn(),
  };
});

const defaultColumns = useEndpointPageStore.getState().columns;

const resetStore = () => {
  useEndpointPageStore.setState({
    ...initialEndpointType,
    columns: defaultColumns,
    page: 1,
    pageSize: 20,
    totalCount: 0,
    criteria: [],
  });
};

const makeEndpoint = (id: string) => ({ getId: () => id } as any);

describe('useEndpointPageStore', () => {
  beforeEach(() => {
    resetStore();
    (GetAllEndpoint as jest.Mock).mockReset();
    (GetEndpoint as jest.Mock).mockReset();
    (CreateEndpointTag as jest.Mock).mockReset();
    (CreateEndpointRetryConfiguration as jest.Mock).mockReset();
    (CreateEndpointCacheConfiguration as jest.Mock).mockReset();
    (UpdateEndpointDetail as jest.Mock).mockReset();
  });

  it('adds, merges, and removes criteria correctly', () => {
    const state = useEndpointPageStore.getState();

    state.addCriteria('status', 'active', 'and');
    state.addCriteria('status', 'paused', 'and');
    state.addCriteria('owner', 'u-1', 'or');

    expect(useEndpointPageStore.getState().criteria).toEqual([
      { key: 'status', value: 'paused', logic: 'and' },
      { key: 'owner', value: 'u-1', logic: 'or' },
    ]);

    state.addCriterias([
      { k: 'status', v: 'ready', logic: 'and' },
      { k: 'model', v: 'gpt-5', logic: 'and' },
    ]);

    expect(useEndpointPageStore.getState().criteria).toEqual([
      { key: 'owner', value: 'u-1', logic: 'or' },
      { key: 'status', value: 'ready', logic: 'and' },
      { key: 'model', value: 'gpt-5', logic: 'and' },
    ]);

    state.removeCriteria('status');
    expect(useEndpointPageStore.getState().criteria).toEqual([
      { key: 'owner', value: 'u-1', logic: 'or' },
      { key: 'model', value: 'gpt-5', logic: 'and' },
    ]);
  });

  it('reloads endpoint to first position and sets current endpoint', () => {
    const endpoint1 = makeEndpoint('e-1');
    const endpoint2 = makeEndpoint('e-2');
    const endpoint2Updated = makeEndpoint('e-2');

    useEndpointPageStore.setState({ endpoints: [endpoint1, endpoint2] });

    useEndpointPageStore.getState().onReloadEndpoint(endpoint2Updated);

    expect(useEndpointPageStore.getState().endpoints).toEqual([
      endpoint2Updated,
      endpoint1,
    ]);
    expect(useEndpointPageStore.getState().currentEndpoint).toBe(endpoint2Updated);
  });

  it('handles successful onGetAllEndpoint response', () => {
    const endpoint = makeEndpoint('endpoint-1');
    const onError = jest.fn();
    const onSuccess = jest.fn();

    (GetAllEndpoint as jest.Mock).mockImplementation(
      (_cfg, _page, _pageSize, _criteria, callback) => {
        callback(null, {
          getSuccess: () => true,
          getDataList: () => [endpoint],
          getPaginated: () => ({ getTotalitem: () => 11 }),
        });
      },
    );

    useEndpointPageStore
      .getState()
      .onGetAllEndpoint('project-1', 'token-1', 'user-1', onError, onSuccess);

    expect(onSuccess).toHaveBeenCalledWith([endpoint]);
    expect(onError).not.toHaveBeenCalled();
    expect(useEndpointPageStore.getState().endpoints).toEqual([endpoint]);
    expect(useEndpointPageStore.getState().totalCount).toBe(11);
  });

  it('uses human-readable error from onGetAllEndpoint response', () => {
    const onError = jest.fn();
    const onSuccess = jest.fn();

    (GetAllEndpoint as jest.Mock).mockImplementation(
      (_cfg, _page, _pageSize, _criteria, callback) => {
        callback(null, {
          getSuccess: () => false,
          getError: () => ({ getHumanmessage: () => 'explicit endpoint error' }),
        });
      },
    );

    useEndpointPageStore
      .getState()
      .onGetAllEndpoint('project-1', 'token-1', 'user-1', onError, onSuccess);

    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith('explicit endpoint error');
  });

  it('uses fallback error when onGetAllEndpoint has no error object', () => {
    const onError = jest.fn();

    (GetAllEndpoint as jest.Mock).mockImplementation(
      (_cfg, _page, _pageSize, _criteria, callback) => {
        callback(null, {
          getSuccess: () => false,
          getError: () => null,
        });
      },
    );

    useEndpointPageStore
      .getState()
      .onGetAllEndpoint('project-1', 'token-1', 'user-1', onError, jest.fn());

    expect(onError).toHaveBeenCalledWith(
      'Something went wrong while retrieving your endpoints. Please refresh the page or try again later.',
    );
  });

  it('handles successful onGetEndpoint response', () => {
    const endpoint = makeEndpoint('endpoint-2');
    const onError = jest.fn();
    const onSuccess = jest.fn();

    (GetEndpoint as jest.Mock).mockImplementation(
      (_cfg, _endpointId, _version, _headers, callback) => {
        callback(null, {
          getSuccess: () => true,
          getData: () => endpoint,
        });
      },
    );

    useEndpointPageStore
      .getState()
      .onGetEndpoint(
        'endpoint-2',
        null,
        'project-1',
        'token-1',
        'user-1',
        onError,
        onSuccess,
      );

    expect(onSuccess).toHaveBeenCalledWith(endpoint);
    expect(onError).not.toHaveBeenCalled();
    expect(useEndpointPageStore.getState().currentEndpoint).toBe(endpoint);
  });

  it('uses fallback error when onGetEndpoint fails without error object', () => {
    const onError = jest.fn();

    (GetEndpoint as jest.Mock).mockImplementation(
      (_cfg, _endpointId, _version, _headers, callback) => {
        callback(null, {
          getSuccess: () => false,
          getError: () => null,
        });
      },
    );

    useEndpointPageStore
      .getState()
      .onGetEndpoint(
        'endpoint-2',
        null,
        'project-1',
        'token-1',
        'user-1',
        onError,
        jest.fn(),
      );

    expect(onError).toHaveBeenCalledWith(
      'Unable to get your endpoint, please try again later.',
    );
  });
});
