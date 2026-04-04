import type { FC, ReactNode } from 'react';
import {
  Tabs as CarbonTabs,
  TabList as CarbonTabList,
  Tab as CarbonTab,
  TabPanels as CarbonTabPanels,
  TabPanel as CarbonTabPanel,
  TabsSkeleton,
} from '@carbon/react';
import { cn } from '@/utils';

// ─── Types ───────────────────────────────────────────────────────────────────

export interface CarbonTabsProps {
  /** Tab labels. */
  tabs?: string[];
  /** Content for each tab. */
  children?: ReactNode | ReactNode[];
  /** Currently selected tab index. */
  selectedIndex?: number;
  /** Called when the selected tab changes. */
  onChange?: (index: number) => void;
  /** Use contained variant (solid background tabs). */
  contained?: boolean;
  /** Accessible label for the tab list. */
  'aria-label'?: string;
  className?: string;
  /** Show TabsSkeleton instead of the real tabs + content. */
  isLoading?: boolean;
}

/** Carbon Tabs — renders tab bar + panels, or a skeleton when loading. */
export const Tabs: FC<CarbonTabsProps> = ({
  tabs = [],
  children,
  selectedIndex = 0,
  onChange,
  contained = false,
  'aria-label': ariaLabel = 'Tabs',
  className,
  isLoading = false,
}) => {
  if (isLoading) {
    return <TabsSkeleton className={cn(className)} />;
  }

  const panels = Array.isArray(children)
    ? children
    : children
      ? [children]
      : [];

  return (
    <CarbonTabs
      selectedIndex={selectedIndex}
      onChange={({ selectedIndex: idx }) => onChange?.(idx)}
    >
      <CarbonTabList contained={contained} aria-label={ariaLabel}>
        {tabs.map(label => (
          <CarbonTab key={label}>{label}</CarbonTab>
        ))}
      </CarbonTabList>
      <CarbonTabPanels>
        {panels.map((panel, idx) => (
          <CarbonTabPanel key={idx} className={cn(className)}>
            {panel}
          </CarbonTabPanel>
        ))}
      </CarbonTabPanels>
    </CarbonTabs>
  );
};

// Re-export raw Carbon components for direct use when needed
export {
  CarbonTabs as RawTabs,
  CarbonTabList as RawTabList,
  CarbonTab as RawTab,
  CarbonTabPanels as RawTabPanels,
  CarbonTabPanel as RawTabPanel,
  TabsSkeleton,
};
