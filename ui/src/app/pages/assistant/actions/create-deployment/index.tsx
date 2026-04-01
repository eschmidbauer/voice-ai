import { Helmet } from '@/app/components/helmet';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { useCurrentCredential } from '@/hooks/use-credential';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import {
  Debug,
  Code,
  Globe,
  Phone,
  Edit,
  Launch,
  View,
  Renew,
  Add,
} from '@carbon/icons-react';
import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  Assistant,
  AssistantDefinition,
  ConnectionConfig,
  GetAssistant,
  GetAssistantRequest,
} from '@rapidaai/react';
import toast from 'react-hot-toast/headless';
import { connectionConfig } from '@/configs';
import { toHumanReadableDateTime } from '@/utils/date';
import { useRapidaStore } from '@/hooks';
import { AssistantPhoneCallDeploymentDialog } from '@/app/components/base/modal/assistant-phone-call-deployment-modal';
import { AssistantDebugDeploymentDialog } from '@/app/components/base/modal/assistant-debug-deployment-modal';
import { AssistantWebWidgetlDeploymentDialog } from '@/app/components/base/modal/assistant-web-widget-deployment-modal';
import { AssistantApiDeploymentDialog } from '@/app/components/base/modal/assistant-api-deployment-modal';
import { CarbonStatusIndicator } from '@/app/components/carbon/status-indicator';
import {
  PrimaryButton,
  SecondaryButton,
  GhostButton,
} from '@/app/components/carbon/button';
import {
  Breadcrumb,
  BreadcrumbItem,
  Button,
  ButtonSet,
  MenuButton,
  MenuItem,
} from '@carbon/react';
import { BaseCard } from '@/app/components/base/cards';

const Info = ({ label, value }: { label: string; value: string }) => (
  <div>
    <dt className="text-[10px] font-medium uppercase tracking-wider text-gray-400 dark:text-gray-500">
      {label}
    </dt>
    <dd className="mt-0.5 text-xs font-medium">{value}</dd>
  </div>
);

export const ConfigureAssistantDeploymentPage = () => {
  const { assistantId } = useParams();
  const [assistant, setAssistant] = useState<Assistant | null>(null);
  const navi = useGlobalNavigation();
  const { token, authId, projectId } = useCurrentCredential();
  const { showLoader, hideLoader } = useRapidaStore();

  const [isExpanded, setIsExpanded] = useState(false);
  const [isApiExpanded, setIsApiExpanded] = useState(false);
  const [isPhoneExpanded, setIsPhoneExpanded] = useState(false);
  const [isWidgetExpanded, setIsWidgetExpanded] = useState(false);

  const get = useCallback(
    (id: typeof assistantId) => {
      if (id) {
        showLoader('block');
        const request = new GetAssistantRequest();
        const assistantDef = new AssistantDefinition();
        assistantDef.setAssistantid(id);
        request.setAssistantdefinition(assistantDef);
        GetAssistant(
          connectionConfig,
          request,
          ConnectionConfig.WithDebugger({
            authorization: token,
            userId: authId,
            projectId: projectId,
          }),
        )
          .then(epmr => {
            hideLoader();
            if (epmr?.getSuccess()) {
              const a = epmr.getData();
              if (a) setAssistant(a);
            } else {
              const error = epmr?.getError();
              toast.error(
                error?.getHumanmessage() ??
                  'Unable to get your assistant. please try again later.',
              );
            }
          })
          .catch(() => hideLoader());
      }
    },
    [token, authId, projectId],
  );

  useEffect(() => {
    get(assistantId);
  }, [assistantId]);

  const deploymentCount = [
    assistant?.getApideployment(),
    assistant?.getWebplugindeployment(),
    assistant?.getDebuggerdeployment(),
    assistant?.getPhonedeployment(),
  ].filter(Boolean).length;

  const hasAnyDeployment = deploymentCount > 0;

  return (
    <div className="flex flex-col w-full flex-1 overflow-auto">
      {/* Modals */}
      {assistant?.getPhonedeployment() && (
        <AssistantPhoneCallDeploymentDialog
          modalOpen={isPhoneExpanded}
          setModalOpen={setIsPhoneExpanded}
          deployment={assistant.getPhonedeployment()!}
        />
      )}
      {assistant?.getDebuggerdeployment() && (
        <AssistantDebugDeploymentDialog
          modalOpen={isExpanded}
          setModalOpen={setIsExpanded}
          deployment={assistant.getDebuggerdeployment()!}
        />
      )}
      {assistant?.getWebplugindeployment() && (
        <AssistantWebWidgetlDeploymentDialog
          modalOpen={isWidgetExpanded}
          setModalOpen={setIsWidgetExpanded}
          deployment={assistant.getWebplugindeployment()!}
        />
      )}
      {assistant?.getApideployment() && (
        <AssistantApiDeploymentDialog
          modalOpen={isApiExpanded}
          setModalOpen={setIsApiExpanded}
          deployment={assistant.getApideployment()!}
        />
      )}

      <Helmet title="Assistant deployment" />

      {/* Page header */}
      <div className="px-4 pt-4 pb-6 border-b border-gray-200 dark:border-gray-800">
        <div className="flex items-start justify-between">
          <div>
            <Breadcrumb noTrailingSlash className="mb-2">
              <BreadcrumbItem
                href={`/deployment/assistant/${assistantId}/overview`}
              >
                Assistant
              </BreadcrumbItem>
            </Breadcrumb>
            <h1 className="text-2xl font-light tracking-tight">Deployments</h1>
          </div>
          <div className="flex items-center gap-2">
            <Button
              hasIconOnly
              renderIcon={Renew}
              iconDescription="Refresh"
              kind="ghost"
              size="md"
              onClick={() => get(assistantId)}
              tooltipPosition="bottom"
            />
            <MenuButton label="Add deployment" size="md" kind="primary">
              <MenuItem
                label="Debugger"
                onClick={() => navi.goToConfigureDebugger(assistantId!)}
              />
              <MenuItem
                label="Web Widget"
                onClick={() => navi.goToConfigureWeb(assistantId!)}
              />
              <MenuItem
                label="SDK / API"
                onClick={() => navi.goToConfigureApi(assistantId!)}
              />
              <MenuItem
                label="Phone Call"
                onClick={() => navi.goToConfigureCall(assistantId!)}
              />
            </MenuButton>
          </div>
        </div>
      </div>

      {hasAnyDeployment ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 content-start gap-4 m-4">
          {/* Debugger */}
          {assistant?.hasDebuggerdeployment() && (
            <DeploymentCard
              icon={<Debug size={24} />}
              title="Debugger"
              description="Internal testing and debugging"
              status="DEPLOYED"
              info={[
                {
                  label: 'Input',
                  value: `Text${assistant.getDebuggerdeployment()?.getInputaudio() ? ' + Audio' : ''}`,
                },
                {
                  label: 'Output',
                  value: `Text${assistant.getDebuggerdeployment()?.getOutputaudio() ? ' + Audio' : ''}`,
                },
                {
                  label: 'Updated',
                  value: toHumanReadableDateTime(
                    assistant.getDebuggerdeployment()?.getCreateddate()!,
                  ),
                },
              ]}
              onEdit={() => navi.goToConfigureDebugger(assistantId!)}
              onPreview={() => navi.goToAssistantPreview(assistantId!)}
              onDetails={() => setIsExpanded(true)}
            />
          )}

          {/* SDK / API */}
          {assistant?.hasApideployment() && (
            <DeploymentCard
              icon={<Code size={24} />}
              title="SDK / API"
              description="Integrate via React SDK or REST API"
              status="DEPLOYED"
              info={[
                { label: 'SDK', value: 'React' },
                {
                  label: 'Input',
                  value: `Text${assistant.getApideployment()?.getInputaudio() ? ' + Audio' : ''}`,
                },
                {
                  label: 'Output',
                  value: `Text${assistant.getApideployment()?.getOutputaudio() ? ' + Audio' : ''}`,
                },
                {
                  label: 'Updated',
                  value: toHumanReadableDateTime(
                    assistant.getApideployment()?.getCreateddate()!,
                  ),
                },
              ]}
              onEdit={() => navi.goToConfigureApi(assistantId!)}
              onDetails={() => setIsApiExpanded(true)}
            />
          )}

          {/* Phone Call */}
          {assistant?.hasPhonedeployment() && (
            <DeploymentCard
              icon={<Phone size={24} />}
              title="Phone Call"
              description="Deploy on inbound or outbound calls"
              status="DEPLOYED"
              info={[
                {
                  label: 'Telephony',
                  value:
                    assistant.getPhonedeployment()?.getPhoneprovidername() ||
                    '—',
                },
                { label: 'Input', value: 'Audio' },
                { label: 'Output', value: 'Audio' },
                {
                  label: 'Updated',
                  value: toHumanReadableDateTime(
                    assistant.getPhonedeployment()?.getCreateddate()!,
                  ),
                },
              ]}
              onEdit={() => navi.goToConfigureCall(assistantId!)}
              onPreview={() => navi.goToAssistantPreviewCall(assistantId!)}
              onDetails={() => setIsPhoneExpanded(true)}
            />
          )}

          {/* Web Widget */}
          {assistant?.hasWebplugindeployment() && (
            <DeploymentCard
              icon={<Globe size={24} />}
              title="Web Widget"
              description="Embed a chat widget on your website"
              status="DEPLOYED"
              info={[
                { label: 'SDK', value: 'JavaScript' },
                {
                  label: 'Input',
                  value: `Text${assistant.getWebplugindeployment()?.getInputaudio() ? ' + Audio' : ''}`,
                },
                {
                  label: 'Output',
                  value: `Text${assistant.getWebplugindeployment()?.getOutputaudio() ? ' + Audio' : ''}`,
                },
                {
                  label: 'Updated',
                  value: toHumanReadableDateTime(
                    assistant.getWebplugindeployment()?.getCreateddate()!,
                  ),
                },
              ]}
              onEdit={() => navi.goToConfigureWeb(assistantId!)}
              onDetails={() => setIsWidgetExpanded(true)}
            />
          )}
        </div>
      ) : (
        <div className="flex flex-col flex-1 items-center justify-center gap-4">
          <EmptyState
            title="No deployments yet"
            subtitle="Add a channel to make your assistant available to users."
          />
          <MenuButton label="Add deployment" size="md" kind="primary">
            <MenuItem
              label="Debugger"
              onClick={() => navi.goToConfigureDebugger(assistantId!)}
            />
            <MenuItem
              label="Web Widget"
              onClick={() => navi.goToConfigureWeb(assistantId!)}
            />
            <MenuItem
              label="SDK / API"
              onClick={() => navi.goToConfigureApi(assistantId!)}
            />
            <MenuItem
              label="Phone Call"
              onClick={() => navi.goToConfigureCall(assistantId!)}
            />
          </MenuButton>
        </div>
      )}
    </div>
  );
};

// ─── Deployment Card ─────────────────────────────────────────────────────────

function DeploymentCard({
  icon,
  title,
  description,
  status,
  info,
  onEdit,
  onPreview,
  onDetails,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
  status: string;
  info: { label: string; value: string }[];
  onEdit: () => void;
  onPreview?: () => void;
  onDetails?: () => void;
}) {
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
      <ButtonSet className="border-t border-gray-200 dark:border-gray-800 [&>button]:!flex-1 [&>button]:!max-w-none">
        {onDetails && (
          <GhostButton size="md" renderIcon={View} onClick={onDetails}>
            Details
          </GhostButton>
        )}
        {onPreview && (
          <SecondaryButton size="md" renderIcon={Launch} onClick={onPreview}>
            Preview
          </SecondaryButton>
        )}
        <PrimaryButton size="md" renderIcon={Edit} onClick={onEdit}>
          Edit
        </PrimaryButton>
      </ButtonSet>
    </BaseCard>
  );
}
