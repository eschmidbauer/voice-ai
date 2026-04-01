import { useRapidaStore } from '@/hooks';
import { FC, useEffect } from 'react';
import toast from 'react-hot-toast/headless';
import { useParams } from 'react-router-dom';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { SelectToolCard } from '@/app/components/base/cards/tool-card';
import { Add, Renew } from '@carbon/icons-react';
import { CreateTool } from '@/app/pages/assistant/actions/configure-assistant-tool/create-assistant-tool';
import { SectionLoader } from '@/app/components/loader/section-loader';
import { useAssistantToolPageStore } from '@/app/pages/assistant/actions/store/use-tool-page-store';
import { useCurrentCredential } from '@/hooks/use-credential';
import { UpdateTool } from '@/app/pages/assistant/actions/configure-assistant-tool/update-assistant-tool';
import { useConfirmDialog } from '@/app/pages/assistant/actions/hooks/use-confirmation';
import { DocNoticeBlock } from '@/app/components/container/message/notice-block/doc-notice-block';
import { PrimaryButton } from '@/app/components/carbon/button';
import { Breadcrumb, BreadcrumbItem, Button } from '@carbon/react';

export function ConfigureAssistantToolPage() {
  const { assistantId } = useParams();
  return (
    <>{assistantId && <ConfigureAssistantTool assistantId={assistantId} />}</>
  );
}

export function CreateAssistantToolPage() {
  const { assistantId } = useParams();
  return <>{assistantId && <CreateTool assistantId={assistantId} />}</>;
}

export function UpdateAssistantToolPage() {
  const { assistantId } = useParams();
  return <>{assistantId && <UpdateTool assistantId={assistantId} />}</>;
}

const ConfigureAssistantTool: FC<{ assistantId: string }> = ({
  assistantId,
}) => {
  const navigator = useGlobalNavigation();
  const navigation = useGlobalNavigation();
  const axtion = useAssistantToolPageStore();
  const { authId, token, projectId } = useCurrentCredential();
  const { loading, showLoader, hideLoader } = useRapidaStore();

  useEffect(() => {
    showLoader('block');
    get();
  }, []);

  const get = () => {
    axtion.getAssistantTool(
      assistantId,
      projectId,
      token,
      authId,
      e => {
        toast.error(e);
        hideLoader();
      },
      v => {
        hideLoader();
      },
    );
  };

  const { showDialog, ConfirmDialogComponent } = useConfirmDialog({
    title: 'Are you sure?',
    content: 'You want to delete? The tool will be removed from assistant.',
  });

  const deleteAssistantTool = (
    assistantId: string,
    assistantToolId: string,
  ) => {
    showLoader('block');
    axtion.deleteAssistantTool(
      assistantId,
      assistantToolId,
      projectId,
      token,
      authId,
      e => {
        toast.error(e);
        hideLoader();
      },
      v => {
        toast.success('Assistant tool deleted successfully');
        get();
      },
    );
  };

  if (loading) {
    return (
      <div className="h-full w-full flex flex-col items-center justify-center">
        <SectionLoader />
      </div>
    );
  }

  return (
    <div className="relative flex flex-col flex-1">
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
            <h1 className="text-2xl font-light tracking-tight">Tools & MCP</h1>
          </div>
          <div className="flex items-center gap-2">
            <Button
              hasIconOnly
              renderIcon={Renew}
              iconDescription="Refresh"
              kind="ghost"
              size="md"
              onClick={() => get()}
              tooltipPosition="bottom"
            />
            <PrimaryButton
              size="md"
              renderIcon={Add}
              onClick={() => navigator.goToCreateAssistantTool(assistantId)}
            >
              Add tool
            </PrimaryButton>
          </div>
        </div>
      </div>

      <DocNoticeBlock docUrl="https://doc.rapida.ai/assistants/tools/">
        Rapida Assistant enables you to call various tools and MCPs to enhance your assistant's capabilities.
      </DocNoticeBlock>

      <div className="overflow-auto flex flex-col flex-1">
        {axtion.tools.length > 0 ? (
          <section className="grid content-start grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4 grow shrink-0 m-4">
            {axtion.tools.map((itm, idx) => (
              <SelectToolCard
                className="col-span-1"
                tool={itm}
                key={`tool-card-${idx}`}
                onEdit={() => {
                  navigation.goToEditAssistantTool(
                    assistantId,
                    itm.getId(),
                  );
                }}
                onDelete={() => {
                  showDialog(() => {
                    deleteAssistantTool(assistantId, itm.getId());
                  });
                }}
              />
            ))}
          </section>
        ) : (
          <div className="my-auto mx-auto">
            <EmptyState
              title="No Tools"
              subtitle="There are no tools added to the assistant"
              action="Add Tools"
              onAction={() => {
                navigation.goToCreateAssistantTool(assistantId);
              }}
            />
          </div>
        )}
      </div>
    </div>
  );
};
