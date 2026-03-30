import { FC } from 'react';
import { Information } from '@carbon/icons-react';
import { Link } from '@carbon/react';

export const DocNoticeBlock: FC<{
  children: React.ReactNode;
  docUrl: string;
  linkText?: string;
}> = ({
  children,
  docUrl,
  linkText = 'Read documentation',
}) => {
  return (
    <div
      className="flex items-center gap-3 w-full px-4 py-3"
      style={{
        backgroundColor: 'var(--cds-notification-info-background-color, #edf5ff)',
        borderLeft: '3px solid var(--cds-support-info, #0043ce)',
      }}
    >
      <Information size={20} className="shrink-0" style={{ color: 'var(--cds-support-info, #0043ce)' }} />
      <span className="text-sm flex-1" style={{ color: 'var(--cds-text-primary)' }}>
        {children}
      </span>
      <Link
        href={docUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="!font-semibold shrink-0"
      >
        {linkText}
      </Link>
    </div>
  );
};
