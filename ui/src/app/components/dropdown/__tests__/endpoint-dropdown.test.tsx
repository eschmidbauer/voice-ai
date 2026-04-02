import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

import { EndpointDropdown } from '@/app/components/dropdown/endpoint-dropdown';
import toast from 'react-hot-toast/headless';

type MockEndpoint = {
  getId: () => string;
  getName: () => string;
};

const makeEndpoint = (id: string, name: string): MockEndpoint => ({
  getId: () => id,
  getName: () => name,
});

const mockAddCriteria = jest.fn();
const mockOnGetAllEndpoint = jest.fn();

let mockEndpoints: MockEndpoint[] = [];
let mockPage = 1;
let mockPageSize = 10;
let mockCriteria: any[] = [];

const mockUseEndpointPageStore = () => ({
  endpoints: mockEndpoints,
  page: mockPage,
  pageSize: mockPageSize,
  criteria: mockCriteria,
  addCriteria: (...args: any[]) => mockAddCriteria(...args),
  onGetAllEndpoint: (...args: any[]) => mockOnGetAllEndpoint(...args),
});

jest.mock('@/hooks', () => ({
  useEndpointPageStore: () => mockUseEndpointPageStore(),
}));

jest.mock('@/hooks/use-credential', () => ({
  useCredential: () => ['auth-1', 'token-1', 'project-1'],
}));

jest.mock('react-hot-toast/headless', () => ({
  __esModule: true,
  default: {
    error: jest.fn(),
  },
}));

jest.mock('@carbon/react', () => ({
  Dropdown: ({
    id,
    label,
    items,
    selectedItem,
    itemToString,
    onChange,
  }: any) => {
    const selectedIndex = Math.max(items.findIndex((x: any) => x === selectedItem), 0);
    return (
      <select
        id={id}
        aria-label={label}
        data-testid={id}
        value={String(selectedIndex)}
        onChange={e => {
          const idx = Number(e.target.value);
          onChange({ selectedItem: items[idx] ?? null });
        }}
      >
        {items.map((item: any, idx: number) => (
          <option key={item.getId?.() || idx} value={String(idx)}>
            {itemToString(item)}
          </option>
        ))}
      </select>
    );
  },
  Button: ({ children, iconDescription, hasIconOnly: _, renderIcon: _r, ...props }: any) => (
    <button aria-label={iconDescription} {...props}>
      {children}
    </button>
  ),
}));

describe('EndpointDropdown', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockEndpoints = [makeEndpoint('e1', 'Endpoint One'), makeEndpoint('e2', 'Endpoint Two')];
    mockPage = 1;
    mockPageSize = 10;
    mockCriteria = [];
  });

  it('fetches endpoints on mount and applies current endpoint criteria when provided', () => {
    const onChangeEndpoint = jest.fn();

    render(
      <EndpointDropdown
        currentEndpoint="e1"
        onChangeEndpoint={onChangeEndpoint as any}
      />,
    );

    expect(mockAddCriteria).toHaveBeenCalledWith('id', 'e1', 'or');
    expect(mockOnGetAllEndpoint).toHaveBeenCalled();
  });

  it('calls onChangeEndpoint when a new endpoint is selected', () => {
    const onChangeEndpoint = jest.fn();

    render(<EndpointDropdown onChangeEndpoint={onChangeEndpoint as any} />);

    fireEvent.change(screen.getByTestId('endpoint-dropdown'), { target: { value: '1' } });
    expect(onChangeEndpoint).toHaveBeenCalledWith(mockEndpoints[1]);
  });

  it('refresh and create endpoint actions trigger expected side effects', () => {
    const onChangeEndpoint = jest.fn();
    const openSpy = jest.spyOn(window, 'open').mockImplementation(() => null);

    render(<EndpointDropdown onChangeEndpoint={onChangeEndpoint as any} />);

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(mockOnGetAllEndpoint).toHaveBeenCalledTimes(2);

    fireEvent.click(screen.getByRole('button', { name: 'Create endpoint' }));
    expect(openSpy).toHaveBeenCalledWith('/deployment/endpoint/create-endpoint', '_blank');

    openSpy.mockRestore();
  });

  it('surfaces fetch errors via toast', () => {
    mockOnGetAllEndpoint.mockImplementation(
      (_projectId, _token, _userId, onError) => onError('boom'),
    );
    const onChangeEndpoint = jest.fn();

    render(<EndpointDropdown onChangeEndpoint={onChangeEndpoint as any} />);

    expect(toast.error).toHaveBeenCalledWith('boom');
  });
});
