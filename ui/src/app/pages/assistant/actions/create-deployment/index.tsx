import { Helmet } from '@/app/components/helmet';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { useCurrentCredential } from '@/hooks/use-credential';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { Renew } from '@carbon/icons-react';
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
import { useRapidaStore } from '@/hooks';
import { AssistantPhoneCallDeploymentDialog } from '@/app/components/base/modal/assistant-phone-call-deployment-modal';
import { AssistantDebugDeploymentDialog } from '@/app/components/base/modal/assistant-debug-deployment-modal';
import { DeploymentEditSectionModal } from '@/app/components/base/modal/assistant-debugger-edit-section-modal';
import { AssistantWebWidgetlDeploymentDialog } from '@/app/components/base/modal/assistant-web-widget-deployment-modal';
import { AssistantApiDeploymentDialog } from '@/app/components/base/modal/assistant-api-deployment-modal';
import {
  DebuggerDeploymentCard,
  ApiDeploymentCard,
  PhoneDeploymentCard,
  WebWidgetDeploymentCard,
} from './cards';
import { ConfigureExperienceModalForm } from '@/app/components/base/modal/assistant-debugger-edit-section-modal/configure-experience-form';
import { ConfigureWebExperienceModalForm } from '@/app/components/base/modal/assistant-debugger-edit-section-modal/configure-web-experience-form';
import { ConfigureAudioInputModalForm } from '@/app/components/base/modal/assistant-debugger-edit-section-modal/configure-audio-input-form';
import { ConfigureAudioOutputModalForm } from '@/app/components/base/modal/assistant-debugger-edit-section-modal/configure-audio-output-form';
import {
  Breadcrumb,
  BreadcrumbItem,
  Button,
  MenuButton,
  MenuItem,
  Checkbox,
} from '@carbon/react';
import { CornerBorderOverlay } from '@/app/components/base/corner-border';
import { useDeploymentSectionEdit } from './hooks/use-deployment-section-edit';

const DEPLOYMENT_LABELS = {
  debugger: 'Debugger',
  api: 'SDK / API',
  web: 'Web Widget',
  phone: 'Phone Call',
} as const;

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

  const sectionEdit = useDeploymentSectionEdit(assistantId, () =>
    get(assistantId),
  );

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
      {sectionEdit.activeEdit && (
        <DeploymentEditSectionModal
          modalOpen={!!sectionEdit.activeEdit}
          setModalOpen={isOpen => {
            if (!isOpen) sectionEdit.closeEditModal();
          }}
          section={sectionEdit.activeEdit.section}
          size={sectionEdit.activeEdit.section === 'experience' ? 'md' : 'lg'}
          label={DEPLOYMENT_LABELS[sectionEdit.activeEdit.type]}
          errorMessage={sectionEdit.editError}
          isSaving={sectionEdit.isSaving}
          onSave={sectionEdit.saveSection}
        >
          {sectionEdit.activeEdit.section === 'experience' &&
            sectionEdit.activeEdit.type === 'web' && (
              <ConfigureWebExperienceModalForm
                experienceConfig={sectionEdit.experienceConfig}
                setExperienceConfig={sectionEdit.setExperienceConfig}
              />
            )}
          {sectionEdit.activeEdit.section === 'experience' &&
            sectionEdit.activeEdit.type !== 'web' && (
              <ConfigureExperienceModalForm
                experienceConfig={sectionEdit.experienceConfig}
                setExperienceConfig={sectionEdit.setExperienceConfig}
              />
            )}
          {sectionEdit.activeEdit.section === 'voice-input' && (
            <div className="space-y-4">
              <button
                type="button"
                onClick={() => sectionEdit.setVoiceInputEnable(!sectionEdit.voiceInputEnable)}
                className="relative group w-full text-left p-4 border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950/50 hover:bg-gray-50 dark:hover:bg-gray-900/60 transition-colors"
              >
                <CornerBorderOverlay
                  className={sectionEdit.voiceInputEnable ? 'opacity-100' : undefined}
                />
                <div onClick={e => e.stopPropagation()}>
                  <Checkbox
                    id="deployment-voice-input-toggle"
                    labelText="Enable voice input (Speech-to-Text)"
                    checked={sectionEdit.voiceInputEnable}
                    onChange={(_, { checked }) =>
                      sectionEdit.setVoiceInputEnable(checked)
                    }
                  />
                </div>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1 ml-6">
                  {sectionEdit.voiceInputEnable
                    ? 'Voice input is currently enabled.'
                    : 'Voice input is disabled. This deployment will not transcribe user speech.'}
                </p>
              </button>
              {sectionEdit.voiceInputEnable && (
                <ConfigureAudioInputModalForm
                  audioInputConfig={sectionEdit.audioInputConfig}
                  setAudioInputConfig={sectionEdit.setAudioInputConfig}
                />
              )}
            </div>
          )}
          {sectionEdit.activeEdit.section === 'voice-output' && (
            <div className="space-y-4">
              <button
                type="button"
                onClick={() => sectionEdit.setVoiceOutputEnable(!sectionEdit.voiceOutputEnable)}
                className="relative group w-full text-left p-4 border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950/50 hover:bg-gray-50 dark:hover:bg-gray-900/60 transition-colors"
              >
                <CornerBorderOverlay
                  className={sectionEdit.voiceOutputEnable ? 'opacity-100' : undefined}
                />
                <div onClick={e => e.stopPropagation()}>
                  <Checkbox
                    id="deployment-voice-output-toggle"
                    labelText="Enable voice output (Text-to-Speech)"
                    checked={sectionEdit.voiceOutputEnable}
                    onChange={(_, { checked }) =>
                      sectionEdit.setVoiceOutputEnable(checked)
                    }
                  />
                </div>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1 ml-6">
                  {sectionEdit.voiceOutputEnable
                    ? 'Voice output is currently enabled.'
                    : 'Voice output is disabled. Assistant responses will be text only.'}
                </p>
              </button>
              {sectionEdit.voiceOutputEnable && (
                <ConfigureAudioOutputModalForm
                  audioOutputConfig={sectionEdit.audioOutputConfig}
                  setAudioOutputConfig={sectionEdit.setAudioOutputConfig}
                />
              )}
            </div>
          )}
        </DeploymentEditSectionModal>
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
          {assistant?.hasDebuggerdeployment() && (
            <DebuggerDeploymentCard
              assistant={assistant}
              onEditSection={section =>
                sectionEdit.openEditModal('debugger', section)
              }
              onPreview={() => navi.goToAssistantPreview(assistantId!)}
              onDetails={() => setIsExpanded(true)}
            />
          )}
          {assistant?.hasApideployment() && (
            <ApiDeploymentCard
              assistant={assistant}
              onEdit={() => navi.goToConfigureApi(assistantId!)}
              onEditSection={section =>
                sectionEdit.openEditModal('api', section)
              }
              onDetails={() => setIsApiExpanded(true)}
            />
          )}
          {assistant?.hasPhonedeployment() && (
            <PhoneDeploymentCard
              assistant={assistant}
              onEdit={() => navi.goToConfigureCall(assistantId!)}
              onEditSection={section =>
                sectionEdit.openEditModal('phone', section as any)
              }
              onPreview={() => navi.goToAssistantPreviewCall(assistantId!)}
              onDetails={() => setIsPhoneExpanded(true)}
            />
          )}
          {assistant?.hasWebplugindeployment() && (
            <WebWidgetDeploymentCard
              assistant={assistant}
              onEdit={() => navi.goToConfigureWeb(assistantId!)}
              onEditSection={section =>
                sectionEdit.openEditModal('web', section)
              }
              onDetails={() => setIsWidgetExpanded(true)}
            />
          )}
        </div>
      ) : (
        <div className="flex flex-col flex-1 items-center justify-center">
          <EmptyState
            title="No deployments yet"
            subtitle="Add a channel to make your assistant available to users."
            actionComponent={
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
            }
          />
        </div>
      )}
    </div>
  );
};

