import { SecondaryButton } from '@/app/components/carbon/button';
import { ExternalLink } from 'lucide-react';

/**
 * External-link button for browsing voices in the model integration page.
 * Used alongside voice dropdowns in TTS provider configuration forms.
 */
export const VoiceBrowseLink: React.FC<{ href: string }> = ({ href }) => (
  <SecondaryButton
    size="md"
    as="a"
    target="_blank"
    href={href}
    className="h-10 text-sm p-2 px-3 bg-light-background max-w-full dark:bg-gray-950 border-b"
  >
    <ExternalLink className="w-4 h-4" strokeWidth={1.5} />
  </SecondaryButton>
);
