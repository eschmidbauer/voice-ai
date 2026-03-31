import { Helmet } from '@/app/components/helmet';
import { useRapidaStore } from '@/hooks';
import { useCredential } from '@/hooks/use-credential';
import { FC, HTMLAttributes, useEffect, useState } from 'react';
import toast from 'react-hot-toast/headless';
import { Outlet, useParams } from 'react-router-dom';
import { cn } from '@/utils';
import {
  AssistantDefinition,
  ConnectionConfig,
  GetAssistant,
  GetAssistantRequest,
} from '@rapidaai/react';
import { useAssistantPageStore } from '@/hooks/use-assistant-page-store';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { ErrorContainer } from '@/app/components/error-container';
import { connectionConfig } from '@/configs';
import { AssistantSideNav } from './assistant-side-nav';

export const AssistantViewLayout: FC<HTMLAttributes<HTMLDivElement>> = () => {
  const [userId, token, projectId] = useCredential();
  const { showLoader, hideLoader } = useRapidaStore();
  const assistantAction = useAssistantPageStore();
  const { assistantId } = useParams();
  const [navExpanded, setNavExpanded] = useState(true);

  const {
    goToAssistantPreview,
    goToAssistantPreviewCall,
    goToCreateAssistantVersion,
    goToCreateAssistantAgentKitVersion,
    goToAssistantListing,
  } = useGlobalNavigation();

  const [unknownState, setUnknownState] = useState(false);

  useEffect(() => {
    assistantAction.clear();
    if (assistantId) {
      showLoader();
      const request = new GetAssistantRequest();
      const assistantDef = new AssistantDefinition();
      assistantDef.setAssistantid(assistantId);
      request.setAssistantdefinition(assistantDef);
      GetAssistant(
        connectionConfig,
        request,
        ConnectionConfig.WithDebugger({
          authorization: token,
          userId: userId,
          projectId: projectId,
        }),
      )
        .then(epmr => {
          hideLoader();
          if (epmr?.getSuccess()) {
            let assistant = epmr.getData();
            if (assistant) assistantAction.onChangeCurrentAssistant(assistant);
          } else {
            setUnknownState(true);
            const error = epmr?.getError();
            if (error) {
              toast.error(error.getHumanmessage());
              return;
            }
            toast.error(
              'Unable to get your assistant. please try again later.',
            );
          }
        })
        .catch(() => {
          hideLoader();
        });
    }
  }, [assistantId]);

  if (unknownState)
    return (
      <div className="flex flex-1">
        <ErrorContainer
          onAction={goToAssistantListing}
          code="403"
          actionLabel="Go to listing"
          title="Assistant not available"
          description="This assistant may be archived or you don't have access to it. Please check with your administrator or try another assistant."
        />
      </div>
    );

  return (
    <div className={cn('flex h-full flex-1 overflow-hidden')}>
      <Helmet title="Hosted Assistant" />

      {/* ── Left side nav (config-driven) ── */}
      {assistantAction.currentAssistant && assistantId && (
        <AssistantSideNav
          assistantId={assistantId}
          assistant={assistantAction.currentAssistant}
          expanded={navExpanded}
          onToggle={() => setNavExpanded(!navExpanded)}
          actions={{
            preview: () => goToAssistantPreview(assistantId),
            'preview-call': () => goToAssistantPreviewCall(assistantId),
          }}
        />
      )}

      {/* ── Main content area ── */}
      <div className="flex flex-col flex-1 overflow-auto">
        <Outlet />
      </div>
    </div>
  );
};
