import React from 'react';
import { Edit, View, Launch } from '@carbon/icons-react';
import { ButtonSet, MenuButton, MenuItem } from '@carbon/react';
import { BaseCard } from '@/app/components/base/cards';
import { CarbonStatusIndicator } from '@/app/components/carbon/status-indicator';
import {
  PrimaryButton,
  SecondaryButton,
  IconOnlyButton,
} from '@/app/components/carbon/button';

const Info = ({ label, value }: { label: string; value: string }) => (
  <div>
    <dt className="text-[10px] font-medium uppercase tracking-wider text-gray-400 dark:text-gray-500">
      {label}
    </dt>
    <dd className="mt-0.5 text-xs font-medium">{value}</dd>
  </div>
);

export interface DeploymentCardProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  status: string;
  info: { label: string; value: string }[];
  onEdit: () => void;
  editMenuItems?: { label: string; renderIcon?: React.ComponentType; onClick: () => void }[];
  onPreview?: () => void;
  onDetails?: () => void;
}

export function DeploymentCard({
  icon,
  title,
  description,
  status,
  info,
  onEdit,
  editMenuItems,
  onPreview,
  onDetails,
}: DeploymentCardProps) {
  return (
    <BaseCard>
      <div className="p-4 space-y-4">
        <div className="flex items-start justify-between">
          <span className="text-gray-600 dark:text-gray-400">{icon}</span>
          <CarbonStatusIndicator state={status} />
        </div>
        <div>
          <p className="text-base font-semibold">{title}</p>
          <p className="mt-0.5 text-sm text-gray-500 dark:text-gray-400">
            {description}
          </p>
        </div>
        <dl className="grid grid-cols-2 gap-x-3 gap-y-3">
          {info.map((item, i) => (
            <Info key={i} label={item.label} value={item.value} />
          ))}
        </dl>
      </div>
      <ButtonSet className="deployment-card-footer border-t border-gray-200 dark:border-gray-800">
        {onDetails && (
          <IconOnlyButton
            kind="ghost"
            size="lg"
            renderIcon={View}
            iconDescription="Details"
            onClick={onDetails}
            className="w-full"
            tooltipPosition="bottom"
          />
        )}
        {onPreview && (
          <SecondaryButton size="md" renderIcon={Launch} onClick={onPreview}>
            Preview
          </SecondaryButton>
        )}
        {editMenuItems?.length ? (
          <MenuButton
            label="Edit"
            size="md"
            kind="primary"
            menuAlignment="bottom-end"
          >
            {editMenuItems.map(item => (
              <MenuItem
                key={item.label}
                label={item.label}
                renderIcon={item.renderIcon}
                onClick={item.onClick}
              />
            ))}
          </MenuButton>
        ) : (
          <PrimaryButton size="md" renderIcon={Edit} onClick={onEdit}>
            Edit
          </PrimaryButton>
        )}
      </ButtonSet>
    </BaseCard>
  );
}
