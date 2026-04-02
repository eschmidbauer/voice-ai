import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';

import { EditAssistant } from '@/app/pages/assistant/actions/edit-assistant';

const mockShowLoader = jest.fn();
const mockHideLoader = jest.fn();

jest.mock('@rapidaai/react', () => {
  class ConnectionConfig {
    constructor(_: unknown) {}

    static WithDebugger(config: unknown) {
      return config;
    }
  }

  class AssistantDefinition {
    setAssistantid(_: string) {}
  }

  class GetAssistantRequest {
    setAssistantdefinition(_: unknown) {}
  }

  const GetAssistant = () =>
    Promise.resolve({
      getSuccess: () => true,
      getData: () => ({
        getName: () => 'Demo Assistant',
        getDescription: () => 'Demo Description',
      }),
    });

  return {
    ConnectionConfig,
    AssistantDefinition,
    GetAssistantRequest,
    GetAssistant,
    UpdateAssistantDetail: jest.fn(),
    DeleteAssistant: jest.fn(),
  };
});

jest.mock('@/hooks/use-credential', () => ({
  useCurrentCredential: () => ({ authId: 'u1', token: 't1', projectId: 'p1' }),
}));

jest.mock('@/hooks', () => ({
  useRapidaStore: () => ({
    loading: false,
    showLoader: mockShowLoader,
    hideLoader: mockHideLoader,
  }),
}));

jest.mock('@/hooks/use-global-navigator', () => ({
  useGlobalNavigation: () => ({
    goToAssistantListing: jest.fn(),
  }),
}));

jest.mock('@/app/pages/assistant/actions/hooks/use-delete-confirmation', () => ({
  useDeleteConfirmDialog: () => ({
    showDialog: jest.fn(),
    ConfirmDeleteDialogComponent: () => null,
  }),
}));

jest.mock('@/app/components/carbon/notification', () => ({
  Notification: ({ subtitle }: any) => <div>{subtitle}</div>,
}));

jest.mock('react-hot-toast/headless', () => ({
  success: jest.fn(),
  error: jest.fn(),
}));

describe('EditAssistant layout', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('uses light/dark background tokens on general settings page root', async () => {
    const { container } = render(<EditAssistant assistantId="assistant-1" />);

    await waitFor(() => {
      expect(screen.getByText('General Settings')).toBeInTheDocument();
    });

    const pageRoot = container.firstElementChild as HTMLElement;
    expect(pageRoot).toHaveClass('bg-white');
    expect(pageRoot).toHaveClass('dark:bg-gray-900');
  });
});
