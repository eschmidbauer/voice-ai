/**
 * Tests that tool configuration components use Carbon Design System components
 * after migration from legacy form components.
 *
 * Reuses the test pattern from:
 *   src/app/components/providers/__tests__/config-renderer.test.tsx
 */
import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Metadata } from '@rapidaai/react';

// ── Mocks ───────────────────────────────────────────────────────────────────

jest.mock('@/utils', () => ({
  cn: (...inputs: any[]) => inputs.filter(Boolean).join(' '),
}));

// Mock Carbon react Select/SelectItem to render native elements
jest.mock('@carbon/react', () => {
  const React = require('react');
  return {
    Select: ({ id, labelText, value, onChange, children, hideLabel }: any) =>
      React.createElement(
        'div',
        null,
        !hideLabel && labelText
          ? React.createElement('label', { htmlFor: id }, labelText)
          : null,
        React.createElement(
          'select',
          { id, value, onChange, 'data-testid': id },
          children,
        ),
      ),
    SelectItem: ({ value, text }: any) =>
      React.createElement('option', { value }, text),
    Slider: ({ id, value, onChange }: any) =>
      React.createElement('input', {
        id,
        type: 'range',
        value: value ?? 0,
        'data-testid': id,
        onChange: (e: any) => onChange?.({ value: Number(e.target.value) }),
      }),
    Tooltip: ({ children }: any) => React.createElement('span', null, children),
  };
});

// Mock Carbon form wrapper components
jest.mock('@/app/components/carbon/form', () => {
  const React = require('react');
  return {
    TextInput: ({ id, labelText, value, onChange, placeholder, type, hideLabel, size }: any) =>
      React.createElement(
        'div',
        null,
        !hideLabel && labelText
          ? React.createElement('label', { htmlFor: id }, labelText)
          : null,
        React.createElement('input', {
          id,
          value: value ?? '',
          onChange,
          placeholder,
          type: type || 'text',
          'data-testid': id,
        }),
      ),
    TextArea: ({ id, labelText, value, onChange, placeholder, rows }: any) =>
      React.createElement(
        'div',
        null,
        labelText
          ? React.createElement('label', { htmlFor: id }, labelText)
          : null,
        React.createElement('textarea', {
          id,
          value: value ?? '',
          onChange,
          placeholder,
          rows,
          'data-testid': id,
        }),
      ),
    Stack: ({ children }: any) =>
      React.createElement('div', null, children),
  };
});

// Mock external dependencies that the tool components use
jest.mock('@/app/components/container/message/notice-block', () => {
  const R = require('react');
  return {
    BlueNoticeBlock: ({ children }: any) =>
      R.createElement('div', { 'data-testid': 'notice-block' }, children),
  };
});

jest.mock('@/app/components/external-api/api-header', () => {
  const R = require('react');
  return {
    APiStringHeader: ({ headerValue }: any) =>
      R.createElement('div', { 'data-testid': 'api-header' }, headerValue || ''),
  };
});

jest.mock('@/app/components/dropdown/knowledge-dropdown', () => {
  const R = require('react');
  return {
    KnowledgeDropdown: () =>
      R.createElement('div', { 'data-testid': 'knowledge-dropdown' }),
  };
});

jest.mock('@/app/components/form/checkbox-card', () => {
  const R = require('react');
  return {
    __esModule: true,
    default: ({ children, id, checked, onChange }: any) =>
      R.createElement(
        'div',
        { 'data-testid': `checkbox-card-${id}`, onClick: onChange },
        children,
      ),
  };
});

jest.mock('@/app/components/base/cards', () => {
  const R = require('react');
  return {
    Card: ({ children, className }: any) =>
      R.createElement('div', { className }, children),
  };
});

jest.mock('@/app/components/form/slider', () => {
  const R = require('react');
  return {
    Slider: ({ value, onSlide }: any) =>
      R.createElement('input', {
        type: 'range',
        value: value ?? 0,
        onChange: (e: any) => onSlide(Number(e.target.value)),
        'data-testid': 'slider',
      }),
  };
});

jest.mock('@/app/components/Icon/hybrid-search', () => {
  const R = require('react');
  return {
    HybridSearchIcon: () => R.createElement('span', null, 'HybridIcon'),
  };
});
jest.mock('@/app/components/Icon/text-search', () => {
  const R = require('react');
  return {
    TextSearchIcon: () => R.createElement('span', null, 'TextIcon'),
  };
});
jest.mock('@/app/components/Icon/vector-search', () => {
  const R = require('react');
  return {
    VectorSearchIcon: () => R.createElement('span', null, 'VectorIcon'),
  };
});
jest.mock('@/app/components/tooltip', () => {
  const R = require('react');
  return {
    Tooltip: ({ children }: any) =>
      R.createElement('span', null, children),
  };
});

jest.mock('@/models/datasets', () => ({
  RETRIEVE_METHOD: {
    hybrid: 'hybrid_search',
    semantic: 'semantic_search',
    fullText: 'full_text_search',
  },
}));

jest.mock('../common', () => {
  const R = require('react');
  const actual = jest.requireActual('../common');
  return {
    ...actual,
    ToolDefinitionForm: ({ toolDefinition }: any) =>
      R.createElement('div', { 'data-testid': 'tool-definition-form' }, toolDefinition?.name),
    ParameterEditor: () =>
      R.createElement('div', { 'data-testid': 'parameter-editor' }),
  };
});

// ── Helpers ─────────────────────────────────────────────────────────────────

function createMetadata(key: string, value: string): Metadata {
  const m = new Metadata();
  m.setKey(key);
  m.setValue(value);
  return m;
}

// ── Tests ───────────────────────────────────────────────────────────────────

describe('ConfigureMCP — Carbon migration', () => {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { ConfigureMCP } = require('../../tools/mcp/index');

  const mockOnParameterChange = jest.fn();
  const mockOnChangeToolDefinition = jest.fn();

  const defaultProps = {
    toolDefinition: { name: 'test-mcp', description: 'desc', parameters: '{}' },
    onChangeToolDefinition: mockOnChangeToolDefinition,
    onParameterChange: mockOnParameterChange,
    parameters: [
      createMetadata('mcp.server_url', 'https://example.com'),
      createMetadata('mcp.protocol', 'sse'),
      createMetadata('mcp.timeout', '30'),
      createMetadata('mcp.headers', ''),
    ],
    inputClass: '',
  };

  beforeEach(() => {
    mockOnParameterChange.mockClear();
    mockOnChangeToolDefinition.mockClear();
  });

  it('renders Carbon TextInput for Name with correct id and labelText', () => {
    render(<ConfigureMCP {...defaultProps} />);
    expect(screen.getByTestId('mcp-tool-name')).toBeInTheDocument();
    expect(screen.getByLabelText('Name')).toBeInTheDocument();
  });

  it('renders Carbon TextArea for Description with correct id', () => {
    render(<ConfigureMCP {...defaultProps} />);
    expect(screen.getByTestId('mcp-tool-description')).toBeInTheDocument();
    expect(screen.getByLabelText('Description')).toBeInTheDocument();
  });

  it('renders Carbon TextInput for MCP Server URL', () => {
    render(<ConfigureMCP {...defaultProps} />);
    expect(screen.getByTestId('mcp-server-url')).toBeInTheDocument();
    expect(screen.getByLabelText('MCP Server URL')).toBeInTheDocument();
  });

  it('renders Carbon Select for Protocol with SelectItem options', () => {
    render(<ConfigureMCP {...defaultProps} />);
    expect(screen.getByTestId('mcp-protocol')).toBeInTheDocument();
    expect(screen.getByLabelText('Protocol')).toBeInTheDocument();
    // Check that options are rendered
    const select = screen.getByTestId('mcp-protocol');
    expect(select.querySelectorAll('option').length).toBe(3);
  });

  it('renders Carbon TextInput for Timeout', () => {
    render(<ConfigureMCP {...defaultProps} />);
    expect(screen.getByTestId('mcp-timeout')).toBeInTheDocument();
    expect(screen.getByLabelText('Timeout (seconds)')).toBeInTheDocument();
  });

  it('calls onChangeToolDefinition when Name input changes', () => {
    render(<ConfigureMCP {...defaultProps} />);
    const nameInput = screen.getByTestId('mcp-tool-name');
    fireEvent.change(nameInput, { target: { value: 'new-name' } });
    expect(mockOnChangeToolDefinition).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'new-name' }),
    );
  });

  it('calls onParameterChange when Server URL changes', () => {
    render(<ConfigureMCP {...defaultProps} />);
    const urlInput = screen.getByTestId('mcp-server-url');
    fireEvent.change(urlInput, { target: { value: 'https://new.example.com' } });
    expect(mockOnParameterChange).toHaveBeenCalled();
    const updatedParams = mockOnParameterChange.mock.calls[0][0] as Metadata[];
    const serverUrl = updatedParams.find(m => m.getKey() === 'mcp.server_url');
    expect(serverUrl?.getValue()).toBe('https://new.example.com');
  });

  // Regression: ensure no old components are rendered (FieldSet, FormLabel, InputGroup)
  it('does not render legacy FieldSet or FormLabel wrappers', () => {
    const { container } = render(<ConfigureMCP {...defaultProps} />);
    expect(container.querySelector('fieldset')).toBeNull();
  });
});

describe('ConfigureAPIRequest — Carbon migration', () => {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { ConfigureAPIRequest } = require('../../tools/api-request/index');

  const mockOnParameterChange = jest.fn();
  const mockOnChangeToolDefinition = jest.fn();

  const defaultProps = {
    toolDefinition: { name: 'test-api', description: 'desc', parameters: '{}' },
    onChangeToolDefinition: mockOnChangeToolDefinition,
    onParameterChange: mockOnParameterChange,
    parameters: [
      createMetadata('tool.method', 'GET'),
      createMetadata('tool.endpoint', 'https://api.example.com'),
      createMetadata('tool.headers', ''),
      createMetadata('tool.parameters', '[]'),
    ],
    inputClass: '',
  };

  beforeEach(() => {
    mockOnParameterChange.mockClear();
    mockOnChangeToolDefinition.mockClear();
  });

  it('renders Carbon Select for Method with correct id and labelText', () => {
    render(<ConfigureAPIRequest {...defaultProps} />);
    expect(screen.getByTestId('api-request-method')).toBeInTheDocument();
    expect(screen.getByLabelText('Method')).toBeInTheDocument();
    const select = screen.getByTestId('api-request-method');
    expect(select.querySelectorAll('option').length).toBe(4);
  });

  it('renders Carbon TextInput for Server URL', () => {
    render(<ConfigureAPIRequest {...defaultProps} />);
    expect(screen.getByTestId('api-request-server-url')).toBeInTheDocument();
    expect(screen.getByLabelText('Server URL')).toBeInTheDocument();
  });

  it('calls onParameterChange when method select changes', () => {
    render(<ConfigureAPIRequest {...defaultProps} />);
    const methodSelect = screen.getByTestId('api-request-method');
    fireEvent.change(methodSelect, { target: { value: 'POST' } });
    expect(mockOnParameterChange).toHaveBeenCalled();
    const updatedParams = mockOnParameterChange.mock.calls[0][0] as Metadata[];
    const method = updatedParams.find(m => m.getKey() === 'tool.method');
    expect(method?.getValue()).toBe('POST');
  });

  it('calls onParameterChange when Server URL changes', () => {
    render(<ConfigureAPIRequest {...defaultProps} />);
    const urlInput = screen.getByTestId('api-request-server-url');
    fireEvent.change(urlInput, { target: { value: 'https://new-api.com' } });
    expect(mockOnParameterChange).toHaveBeenCalled();
    const updatedParams = mockOnParameterChange.mock.calls[0][0] as Metadata[];
    const endpoint = updatedParams.find(m => m.getKey() === 'tool.endpoint');
    expect(endpoint?.getValue()).toBe('https://new-api.com');
  });

  // Regression: no legacy form components
  it('does not render legacy FieldSet elements', () => {
    const { container } = render(<ConfigureAPIRequest {...defaultProps} />);
    expect(container.querySelector('fieldset')).toBeNull();
  });
});

describe('ConfigureKnowledgeRetrieval — Carbon migration', () => {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { ConfigureKnowledgeRetrieval } = require('../../tools/knowledge-retrieval/index');

  const mockOnParameterChange = jest.fn();
  const mockOnChangeToolDefinition = jest.fn();

  const defaultProps = {
    toolDefinition: { name: 'test-kr', description: 'desc', parameters: '{}' },
    onChangeToolDefinition: mockOnChangeToolDefinition,
    onParameterChange: mockOnParameterChange,
    parameters: [
      createMetadata('tool.knowledge_id', 'k1'),
      createMetadata('tool.search_type', 'hybrid_search'),
      createMetadata('tool.top_k', '5'),
      createMetadata('tool.score_threshold', '0.7'),
    ],
    inputClass: '',
  };

  beforeEach(() => {
    mockOnParameterChange.mockClear();
    mockOnChangeToolDefinition.mockClear();
  });

  it('renders Carbon TextInput for slider numeric fields (hidden label)', () => {
    render(<ConfigureKnowledgeRetrieval {...defaultProps} />);
    expect(screen.getByTestId('top-k')).toBeInTheDocument();
    expect(screen.getByTestId('score-threshold')).toBeInTheDocument();
  });

  it('renders retrieval setting label as plain text instead of FormLabel', () => {
    render(<ConfigureKnowledgeRetrieval {...defaultProps} />);
    expect(screen.getByText('Retrieval setting')).toBeInTheDocument();
  });

  it('renders search type cards', () => {
    render(<ConfigureKnowledgeRetrieval {...defaultProps} />);
    expect(screen.getByText('Hybrid Search')).toBeInTheDocument();
    expect(screen.getByText('Semantic Search')).toBeInTheDocument();
    expect(screen.getByText('Full Text Search')).toBeInTheDocument();
  });

  it('calls onParameterChange when top_k numeric input changes', () => {
    render(<ConfigureKnowledgeRetrieval {...defaultProps} />);
    const topKInput = screen.getByTestId('top-k');
    fireEvent.change(topKInput, { target: { value: '8' } });
    expect(mockOnParameterChange).toHaveBeenCalled();
    const updatedParams = mockOnParameterChange.mock.calls[0][0] as Metadata[];
    const topK = updatedParams.find(m => m.getKey() === 'tool.top_k');
    expect(topK?.getValue()).toBe('8');
  });

  // Regression: no legacy FieldSet or InputGroup
  it('does not render legacy FieldSet elements', () => {
    const { container } = render(<ConfigureKnowledgeRetrieval {...defaultProps} />);
    expect(container.querySelector('fieldset')).toBeNull();
  });

  it('renders ToolDefinitionForm when toolDefinition is provided', () => {
    render(<ConfigureKnowledgeRetrieval {...defaultProps} />);
    expect(screen.getByTestId('tool-definition-form')).toBeInTheDocument();
  });
});
