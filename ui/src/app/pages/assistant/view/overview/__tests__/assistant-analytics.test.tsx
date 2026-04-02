import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';

const mockGoToAssistantSessionList = jest.fn();
const mockGetAssistantMessages = jest.fn();

jest.mock('@/hooks/use-global-navigator', () => ({
  useGlobalNavigation: () => ({
    goToAssistantSessionList: mockGoToAssistantSessionList,
  }),
}));

jest.mock('@/hooks/use-credential', () => ({
  useCurrentCredential: () => ({
    authId: 'auth-1',
    token: 'token-1',
    projectId: 'project-1',
  }),
}));

jest.mock('@/configs', () => ({
  connectionConfig: {},
}));

jest.mock('@/hooks/use-assistant-trace-page-store', () => ({
  useAssistantTracePageStore: () => ({
    assistantMessages: [
      {
        getAssistantconversationid: () => 'conv-1',
        getMetricsList: () => [],
        getMetadataList: () => [],
        getSource: () => 'web',
        getMessageid: () => 'user-1',
        getRole: () => 'user',
        getCreateddate: () => ({
          getSeconds: () => 1710000000,
          getNanos: () => 0,
        }),
      },
    ],
    criteria: [],
    clear: jest.fn(),
    addCriterias: jest.fn(),
    setPageSize: jest.fn(),
    setFields: jest.fn(),
    getAssistantMessages: mockGetAssistantMessages,
  }),
}));

jest.mock('@/utils/metadata', () => ({
  getStatusMetric: jest.fn(),
  getTotalTokenMetric: () => 0,
  findMetricByName: () => '',
  isConversationCompleted: () => false,
}));

jest.mock('recharts', () => {
  const Container = ({ children }: any) => <div>{children}</div>;
  const Null = () => null;
  return {
    XAxis: Null,
    Tooltip: Null,
    ResponsiveContainer: Container,
    PieChart: Null,
    Pie: Null,
    Cell: Null,
    Bar: Null,
    BarChart: Null,
    YAxis: Null,
    AreaChart: Null,
    Area: Null,
  };
});

jest.mock('@/app/components/carbon/dropdown', () => ({
  Dropdown: ({ label }: any) => <div>{label}</div>,
}));

jest.mock('@/app/components/carbon/tile', () => ({
  Tile: ({ children }: any) => <div>{children}</div>,
}));

jest.mock('@carbon/react', () => ({
  Button: ({ children, ...props }: any) => <button {...props}>{children}</button>,
  Toggletip: ({ children }: any) => <span>{children}</span>,
  ToggletipButton: ({ label }: any) => <button type="button">{label}</button>,
  ToggletipContent: ({ children }: any) => {
    const nodes = require('react').Children.toArray(children);
    return <span>{nodes[nodes.length - 1]}</span>;
  },
  ToggletipLabel: ({ children }: any) => <span>{children}</span>,
}));

jest.mock('@rapidaai/react', () => ({
  GetAllAssistantConversation: jest.fn(
    (
      _config,
      _assistantId,
      _page,
      _pageSize,
      _criteria,
      callback,
    ) => {
      callback(null, {
        getSuccess: () => true,
        getDataList: () => [{ getMetricsList: () => [] }],
      });
    },
  ),
}));

const { AssistantAnalytics } = require('@/app/pages/assistant/view/overview/assistant-analytics');

describe('AssistantAnalytics sessions toggletip', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockGetAssistantMessages.mockImplementation(
      (
        _assistantId: string,
        _projectId: string,
        _token: string,
        _authId: string,
        onSuccess: () => void,
      ) => onSuccess(),
    );
  });

  it('shows sessions toggletip action and navigates to sessions page', async () => {
    const assistant = { getId: () => 'assistant-1' } as any;
    render(<AssistantAnalytics assistant={assistant} />);

    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: 'Go to sessions' }),
      ).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Go to sessions' }));

    expect(mockGoToAssistantSessionList).toHaveBeenCalledWith('assistant-1');
  });

  it('keeps sessions navigation action scoped to sessions metric', async () => {
    const assistant = { getId: () => 'assistant-1' } as any;
    render(<AssistantAnalytics assistant={assistant} />);

    await waitFor(() => {
      expect(screen.getByText('Active')).toBeInTheDocument();
    });

    expect(screen.getAllByRole('button', { name: 'Go to sessions' })).toHaveLength(1);
  });
});
