import React, { FC, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { toHumanReadableDateTime } from '@/utils/date';
import { Add, Renew } from '@carbon/icons-react';
import { useCurrentCredential } from '@/hooks/use-credential';
import { useRapidaStore } from '@/hooks';
import { SectionLoader } from '@/app/components/loader/section-loader';
import toast from 'react-hot-toast/headless';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { CreateAssistantAnalysis } from '@/app/pages/assistant/actions/configure-assistant-analysis/create-assistant-analysis';
import { useAssistantAnalysisPageStore } from '@/app/pages/assistant/actions/store/use-analysis-page-store';
import { UpdateAssistantAnalysis } from '@/app/pages/assistant/actions/configure-assistant-analysis/update-assistant-analysis';
import { CarbonStatusIndicator } from '@/app/components/carbon/status-indicator';
import { PrimaryButton } from '@/app/components/carbon/button';
import { Pagination } from '@/app/components/carbon/pagination';
import {
  OverflowMenu,
  OverflowMenuItem,
} from '@/app/components/carbon/overflow-menu';
import {
  Breadcrumb,
  BreadcrumbItem,
  Button,
  Table,
  TableHead,
  TableRow,
  TableHeader,
  TableBody,
  TableCell,
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
} from '@carbon/react';
import { TableSection } from '@/app/components/sections/table-section';
import { CustomLink } from '@/app/components/custom-link';

export function ConfigureAssistantAnalysisPage() {
  const { assistantId } = useParams();
  return (
    <>
      {assistantId && <ConfigureAssistantAnalysis assistantId={assistantId} />}
    </>
  );
}

export function CreateAssistantAnalysisPage() {
  const { assistantId } = useParams();
  return (
    <>{assistantId && <CreateAssistantAnalysis assistantId={assistantId} />}</>
  );
}

export function UpdateAssistantAnalysisPage() {
  const { assistantId } = useParams();
  return (
    <>{assistantId && <UpdateAssistantAnalysis assistantId={assistantId} />}</>
  );
}

const ConfigureAssistantAnalysis: FC<{ assistantId: string }> = ({
  assistantId,
}) => {
  const navigation = useGlobalNavigation();
  const axtion = useAssistantAnalysisPageStore();
  const { authId, token, projectId } = useCurrentCredential();
  const { loading, showLoader, hideLoader } = useRapidaStore();

  const get = () => {
    showLoader('block');
    axtion.getAssistantAnalysis(
      assistantId, projectId, token, authId,
      e => { toast.error(e); hideLoader(); },
      () => { hideLoader(); },
    );
  };

  useEffect(() => {
    get();
  }, []);

  const deleteAssistantAnalysis = (assistantId: string, analysisId: string) => {
    showLoader('block');
    axtion.deleteAssistantAnalysis(
      assistantId, analysisId, projectId, token, authId,
      e => { toast.error(e); hideLoader(); },
      () => { get(); },
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
    <div className="h-full flex flex-col flex-1">
      {/* Page header */}
      <div className="px-4 pt-4 pb-6 border-b border-gray-200 dark:border-gray-800">
        <div className="flex items-start justify-between">
          <div>
            <Breadcrumb noTrailingSlash className="mb-2">
              <BreadcrumbItem href={`/deployment/assistant/${assistantId}/overview`}>
                Assistant
              </BreadcrumbItem>
            </Breadcrumb>
            <h1 className="text-2xl font-light tracking-tight">Analysis</h1>
          </div>
        </div>
      </div>

      {/* Toolbar */}
      <TableToolbar>
        <TableToolbarContent>
          <TableToolbarSearch placeholder="Search analysis..." />
          <Button
            hasIconOnly
            renderIcon={Renew}
            iconDescription="Refresh"
            kind="ghost"
            onClick={get}
            tooltipPosition="bottom"
          />
          <PrimaryButton
            size="md"
            renderIcon={Add}
            onClick={() => navigation.goToCreateAssistantAnalysis(assistantId)}
          >
            Create analysis
          </PrimaryButton>
        </TableToolbarContent>
      </TableToolbar>

      {/* Content */}
      <TableSection>
        {axtion.analysises.length > 0 ? (
          <>
            <Table>
              <TableHead>
                <TableRow>
                  <TableHeader>Name</TableHeader>
                  <TableHeader>Endpoint</TableHeader>
                  <TableHeader>Version</TableHeader>
                  <TableHeader>Priority</TableHeader>
                  <TableHeader>Status</TableHeader>
                  <TableHeader>Created</TableHeader>
                  <TableHeader />
                </TableRow>
              </TableHead>
              <TableBody>
                {axtion.analysises.map((row, idx) => (
                  <TableRow key={idx}>
                    <TableCell>
                      <CustomLink
                        to={`/deployment/assistant/${assistantId}/configure-analysis/${row.getId()}`}
                        className="text-primary hover:underline"
                      >
                        {row.getName()}
                      </CustomLink>
                    </TableCell>
                    <TableCell>
                      <CustomLink
                        to={`/deployment/endpoint/${row.getEndpointid()}`}
                        className="text-primary hover:underline font-mono text-xs"
                      >
                        {row.getEndpointid()}
                      </CustomLink>
                    </TableCell>
                    <TableCell>{row.getEndpointversion()}</TableCell>
                    <TableCell>{row.getExecutionpriority()}</TableCell>
                    <TableCell>
                      <CarbonStatusIndicator state={row.getStatus()} />
                    </TableCell>
                    <TableCell>
                      {row.getCreateddate() && toHumanReadableDateTime(row.getCreateddate()!)}
                    </TableCell>
                    <TableCell>
                      <OverflowMenu size="sm" flipped iconDescription="Actions">
                        <OverflowMenuItem
                          itemText="Edit"
                          onClick={() => navigation.goToEditAssistantAnalysis(assistantId, row.getId())}
                        />
                        <OverflowMenuItem
                          itemText="Delete"
                          isDelete
                          hasDivider
                          onClick={() => deleteAssistantAnalysis(assistantId, row.getId())}
                        />
                      </OverflowMenu>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <Pagination
              totalItems={axtion.totalCount}
              page={axtion.page}
              pageSize={axtion.pageSize}
              pageSizes={[10, 20, 50]}
              onChange={({ page, pageSize }) => {
                if (pageSize !== axtion.pageSize) axtion.setPageSize(pageSize);
                else axtion.setPage(page);
              }}
            />
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState
              title="No Analysis"
              subtitle="There are no assistant analysis."
              action="Create new analysis"
              onAction={() => navigation.goToCreateAssistantAnalysis(assistantId)}
            />
          </div>
        )}
      </TableSection>
    </div>
  );
};
