import type { FC } from 'react';
import { Tag } from '@carbon/react';
import { Ai } from '@carbon/icons-react';

const providerLabels: Record<string, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  google: 'Google',
  gemini: 'Gemini',
  azure: 'Azure',
  'azure-openai': 'Azure OpenAI',
  groq: 'Groq',
  mistral: 'Mistral',
  cohere: 'Cohere',
  deepseek: 'DeepSeek',
};

export const ProviderTag: FC<{ provider?: string }> = ({ provider }) => {
  const key = provider?.toLowerCase() || '';
  const label = providerLabels[key] || provider || 'Unknown';

  return (
    <Tag size="md" type="cool-gray">
      <span className="inline-flex items-center gap-1.5 leading-none">
        <Ai size={16} />
        {label}
      </span>
    </Tag>
  );
};
