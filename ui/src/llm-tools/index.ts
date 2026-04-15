export const BUILDIN_TOOLS = [
  {
    icon: 'https://cdn-01.rapida.ai/partners/tools/knowledge-retrieval.webp',
    code: 'knowledge_retrieval',
    name: 'Knowledge Retrieval',
  },
  {
    icon: 'https://cdn-01.rapida.ai/partners/tools/api_call.png',
    code: 'api_request',
    name: 'API request',
  },
  {
    icon: 'https://cdn-01.rapida.ai/partners/tools/api_call.png',
    code: 'endpoint',
    name: 'Endpoint (LLM Call)',
  },
  {
    icon: 'https://cdn-01.rapida.ai/partners/tools/waiting.png',
    code: 'put_on_hold',
    name: 'Put on hold',
  },
  {
    icon: 'https://cdn-01.rapida.ai/partners/tools/stop.png',
    code: 'end_of_conversation',
    name: 'End of conversation',
  },
  {
    icon: 'https://cdn-01.rapida.ai/partners/tools/api_call.png',
    code: 'transfer_call',
    name: 'Transfer call',
  },
  {
    icon: 'https://cdn-01.rapida.ai/partners/tools/api_call.png',
    code: 'mcp',
    name: 'MCP Server',
  },

  // Add more tools as needed
];

export const KnowledgeRetrievalToolDefintion = {
  name: 'knowledge_query',
  description:
    'Use this tool to retrieve specific information or data from provided queries before responding.',
  parameters: JSON.stringify(
    {
      properties: {
        context: {
          description:
            'Concise and searchable description of the users query or topic.',
          type: 'string',
        },
        organizations: {
          description:
            'Names of organizations or companies mentioned in the content',
          items: {
            type: 'string',
          },
          type: 'array',
        },
        products: {
          description: 'Names of products or services mentioned in the content',
          items: {
            type: 'string',
          },
          type: 'array',
        },
      },
      required: ['context'],
      type: 'object',
    },
    null,
    2,
  ),
};

export const APIRequestToolDefintion = {
  name: 'api_call',
  description:
    'Use it to perform API calls by specifying the URL, HTTP verb, and other configurations. Parameters should be simple key-value pairs.',
  parameters: JSON.stringify(
    {
      properties: {
        context: {
          description:
            'Concise and searchable description of the users query or topic.',
          type: 'string',
        },
        organizations: {
          description:
            'Names of organizations or companies mentioned in the content',
          items: {
            type: 'string',
          },
          type: 'array',
        },
        products: {
          description: 'Names of products or services mentioned in the content',
          items: {
            type: 'string',
          },
          type: 'array',
        },
      },
      required: ['context'],
      type: 'object',
    },
    null,
    2,
  ),
};

export const EndOfConverstaionToolDefintion = {
  name: 'end_conversation',
  description: 'Call this function to end the conversation.',
  parameters: JSON.stringify(
    {
      properties: {
        reason: {
          description:
            "Brief reason for ending the conversation, such as 'user said goodbye', 'conversation completed', or 'timeout'.",
          type: 'string',
        },
      },
      required: ['reason'],
      type: 'object',
    },
    null,
    2,
  ),
};

export const TransferCallToolDefintion = {
  name: 'transfer_call',
  description:
    'Call this function to transfer the ongoing call to the configured destination.',
  parameters: JSON.stringify(
    {
      type: 'object',
      properties: {
        reason: {
          description:
            "Brief reason for the transfer, such as 'customer requested human agent' or 'escalation needed'.",
          type: 'string',
        },
      },
    },
    null,
    2,
  ),
};

export const EndpointToolDefintion = {
  name: 'llm_call',
  description:
    'Use it to make calls to a Language Learning Model. Specify the prompt and optional parameters such as temperature or max_tokens.',
  parameters: JSON.stringify(
    {
      properties: {
        prompt: {
          description: 'The input text or prompt for the LLM.',
          type: 'string',
        },
      },
      required: ['prompt'],
      type: 'object',
    },
    null,
    2,
  ),
};
