import React, { FC, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { toHumanReadableDateTime } from '@/utils/date';
import { Activity, Edit, TrashCan, Add, Renew } from '@carbon/icons-react';
import { useCurrentCredential } from '@/hooks/use-credential';
import { SectionLoader } from '@/app/components/loader/section-loader';
import toast from 'react-hot-toast/headless';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { CreateAssistantTelemetry } from './create-assistant-telemetry';
import { UpdateAssistantTelemetry } from './update-assistant-telemetry';
import { useAssistantTelemetryPageStore } from '@/app/pages/assistant/actions/store/use-telemetry-page-store';
import { useConfirmDialog } from '@/app/pages/assistant/actions/hooks/use-confirmation';
import { BaseCard } from '@/app/components/base/cards';
import { TELEMETRY_PROVIDER } from '@/providers';
import { CarbonStatusIndicator } from '@/app/components/carbon/status-indicator';
import { PrimaryButton, GhostButton, DangerButton } from '@/app/components/carbon/button';
import { Breadcrumb, BreadcrumbItem, Button, ButtonSet } from '@carbon/react';

export function ConfigureAssistantTelemetryPage() {
  const { assistantId } = useParams();
  return (
    <>
      {assistantId && <ConfigureAssistantTelemetry assistantId={assistantId} />}
    </>
  );
}

export function CreateAssistantTelemetryPage() {
  const { assistantId } = useParams();
  return (
    <>{assistantId && <CreateAssistantTelemetry assistantId={assistantId} />}</>
  );
}

export function UpdateAssistantTelemetryPage() {
  const { assistantId } = useParams();
  return (
    <>{assistantId && <UpdateAssistantTelemetry assistantId={assistantId} />}</>
  );
}

const providerNameByCode = new Map(
  TELEMETRY_PROVIDER.map(p => [p.code, p.name]),
);

const Info = ({ label, value }: { label: string; value: string }) => (
  <div>
    <dt className="text-[10px] font-medium uppercase tracking-wider text-gray-400 dark:text-gray-500">
      {label}
    </dt>
    <dd className="mt-0.5 text-xs font-medium">{value}</dd>
  </div>
);

const ConfigureAssistantTelemetry: FC<{ assistantId: string }> = ({
  assistantId,
}) => {
  const navigation = useGlobalNavigation();
  const action = useAssistantTelemetryPageStore();
  const { authId, token, projectId } = useCurrentCredential();
  const [loading, setLoading] = useState(true);
  const { showDialog, ConfirmDialogComponent } = useConfirmDialog({
    title: 'Delete telemetry?',
    content: 'This telemetry provider will be removed from the assistant.',
  });

  useEffect(() => {
    get();
  }, []);

  const get = () => {
    setLoading(true);
    action.getAssistantTelemetry(
      assistantId, projectId, token, authId,
      e => { toast.error(e); setLoading(false); },
      () => { setLoading(false); },
    );
  };

  const deleteTelemetry = (telemetryId: string) => {
    setLoading(true);
    action.deleteAssistantTelemetry(
      assistantId, telemetryId, projectId, token, authId,
      e => { toast.error(e); setLoading(false); },
      () => { toast.success('Telemetry provider deleted successfully'); get(); },
    );
  };

  return (
    <div className="flex flex-col w-full flex-1 overflow-auto">
      <ConfirmDialogComponent />

      {/* Page header */}
      <div className="px-4 pt-4 pb-6 border-b border-gray-200 dark:border-gray-800">
        <div className="flex items-start justify-between">
          <div>
            <Breadcrumb noTrailingSlash className="mb-2">
              <BreadcrumbItem href={`/deployment/assistant/${assistantId}/overview`}>
                Assistant
              </BreadcrumbItem>
            </Breadcrumb>
            <h1 className="text-2xl font-light tracking-tight">Telemetry</h1>
          </div>
          <div className="flex items-center gap-2">
            <Button
              hasIconOnly
              renderIcon={Renew}
              iconDescription="Refresh"
              kind="ghost"
              size="md"
              onClick={get}
              tooltipPosition="bottom"
            />
            <PrimaryButton
              size="md"
              renderIcon={Add}
              onClick={() => navigation.goToCreateAssistantTelemetry(assistantId)}
            >
              Add telemetry
            </PrimaryButton>
          </div>
        </div>
      </div>

      {loading ? (
        <div className="flex flex-col flex-1 items-center justify-center">
          <SectionLoader />
        </div>
      ) : action.telemetries.length > 0 ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 content-start m-4">
          {action.telemetries.map(row => {
            const providerType = row.getProvidertype();
            const providerName = providerNameByCode.get(providerType) || providerType;
            const isEnabled = row.getEnabled();

            return (
              <BaseCard key={row.getId()}>
                <div className="flex-1 p-4 md:p-5 space-y-4">
                  <div className="flex items-start justify-between">
                    <Activity size={24} className="text-blue-600" />
                    <CarbonStatusIndicator state={isEnabled ? 'CONNECTED' : 'INACTIVE'} />
                  </div>
                  <div>
                    <p className="text-base font-semibold">{providerName}</p>
                    <p className="mt-0.5 text-sm text-gray-500 dark:text-gray-400">
                      {isEnabled ? 'Enabled' : 'Disabled'} telemetry exporter
                    </p>
                  </div>
                  <dl className="grid grid-cols-3 gap-x-3 gap-y-3 pt-1">
                    <Info label="Provider" value={providerType} />
                    <Info label="Options" value={String(row.getOptionsList().length)} />
                    <Info
                      label="Created"
                      value={row.getCreateddate() ? toHumanReadableDateTime(row.getCreateddate()!) : '—'}
                    />
                  </dl>
                </div>
                <ButtonSet className="border-t border-gray-200 dark:border-gray-800 [&>button]:!flex-1 [&>button]:!max-w-none">
                  <GhostButton
                    size="md"
                    renderIcon={TrashCan}
                    onClick={() => showDialog(() => deleteTelemetry(row.getId()))}
                  >
                    Delete
                  </GhostButton>
                  <PrimaryButton
                    size="md"
                    renderIcon={Edit}
                    onClick={() => navigation.goToEditAssistantTelemetry(assistantId, row.getId())}
                  >
                    Edit
                  </PrimaryButton>
                </ButtonSet>
              </BaseCard>
            );
          })}
        </div>
      ) : (
        <div className="flex flex-col flex-1 items-center justify-center">
          <EmptyState
            title="No telemetry providers"
            subtitle="Add a telemetry destination to export events and metrics from this assistant."
            action="Add telemetry"
            onAction={() => navigation.goToCreateAssistantTelemetry(assistantId)}
          />
        </div>
      )}
    </div>
  );
};
