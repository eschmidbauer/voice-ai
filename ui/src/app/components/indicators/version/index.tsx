import { useState } from 'react';
import { Tag } from '@carbon/react';
import { Checkmark, Copy } from '@carbon/icons-react';
import { IconOnlyButton } from '@/app/components/carbon/button';

export function VersionIndicator({ id }: { id: string }) {
  const [copied, setCopied] = useState(false);
  const version = `vrsn_${id}`;

  const copyItem = () => {
    setCopied(true);
    navigator.clipboard.writeText(version);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <span className="inline-flex items-center gap-1">
      <Tag size="md" type="cool-gray">
        <span className="font-mono leading-none">{version}</span>
      </Tag>
      <IconOnlyButton
        kind="ghost"
        size="sm"
        renderIcon={copied ? Checkmark : Copy}
        iconDescription={copied ? 'Copied' : 'Copy version'}
        onClick={copyItem}
      />
    </span>
  );
}
