import React, { useCallback, useEffect, useState } from 'react';
import { Helmet } from '@/app/components/helmet';
import { useCredential } from '@/hooks/use-credential';
import { useRapidaStore } from '@/hooks';
import { useNavigate, useSearchParams } from 'react-router-dom';
import toast from 'react-hot-toast/headless';
import SingleAssistant from './single-assistant';
import { useAssistantPageStore } from '@/hooks/use-assistant-page-store';
import { Assistant } from '@rapidaai/react';
import { PageLoading } from '@/app/components/carbon/loading';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { Pagination } from '@/app/components/carbon/pagination';
import { Renew } from '@carbon/icons-react';
import {
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
  Button,
  ComboButton,
  MenuItem,
} from '@carbon/react';
import { PageHeaderBlock } from '@/app/components/blocks/page-header-block';
import { PageTitleBlock } from '@/app/components/blocks/page-title-block';

export function AssistantPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [userId, token, projectId] = useCredential();
  const assistantAction = useAssistantPageStore();
  const { loading, showLoader, hideLoader } = useRapidaStore();

  useEffect(() => {
    if (searchParams) {
      const searchParamMap = Object.fromEntries(searchParams.entries());
      Object.entries(searchParamMap).forEach(([key, value]) =>
        assistantAction.addCriteria(key, value, '='),
      );
    }
  }, [searchParams]);

  const onError = useCallback((err: string) => {
    hideLoader();
    toast.error(err);
  }, []);

  const onSuccess = useCallback((data: Assistant[]) => {
    hideLoader();
  }, []);

  const getAssistants = useCallback((projectId, token, userId) => {
    showLoader();
    assistantAction.onGetAllAssistant(
      projectId,
      token,
      userId,
      onError,
      onSuccess,
    );
  }, []);

  useEffect(() => {
    getAssistants(projectId, token, userId);
  }, [
    projectId,
    assistantAction.page,
    assistantAction.pageSize,
    assistantAction.criteria,
  ]);

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <Helmet title="Assistant" />
      <PageHeaderBlock>
        <div className="flex items-center gap-3">
          <PageTitleBlock>Assistants</PageTitleBlock>
          <span className="text-xs text-gray-500 dark:text-gray-400 tabular-nums">
            {assistantAction.pageSize}/{assistantAction.totalCount}
          </span>
        </div>
      </PageHeaderBlock>
      <TableToolbar>
        <TableToolbarContent>
          <TableToolbarSearch placeholder="Search assistants..." />
          <Button
            hasIconOnly
            renderIcon={Renew}
            iconDescription="Refresh"
            kind="ghost"
            onClick={() => getAssistants(projectId, token, userId)}
            tooltipPosition="bottom"
          />
          <ComboButton
            label="Create new Assistant"
            onClick={() => navigate('/deployment/assistant/create-assistant')}
          >
            <MenuItem
              label="Connect new AgentKit"
              onClick={() => navigate('/deployment/assistant/connect-agentkit')}
            />
          </ComboButton>
        </TableToolbarContent>
      </TableToolbar>

      {/* Content */}
      {loading ? (
        <PageLoading className="h-full" />
      ) : assistantAction.assistants && assistantAction.assistants.length > 0 ? (
        <section className="grid content-start grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 gap-4 flex-1 overflow-auto p-4">
          {assistantAction.assistants.map((ast, idx) => (
            <SingleAssistant key={idx} assistant={ast} />
          ))}
        </section>
      ) : assistantAction.criteria.length > 0 ? (
        <div className="h-full flex justify-center items-center">
          <EmptyState
            title="No Assistant"
            subtitle="There are no assistant matching with your criteria."
            action="Create new Assistant"
            onAction={() =>
              navigate('/deployment/assistant/create-assistant')
            }
          />
        </div>
      ) : (
        <div className="h-full flex justify-center items-center">
          <EmptyState
            title="No Assistant"
            subtitle="There are no Assistants to display"
            action="Create new Assistant"
            onAction={() =>
              navigate('/deployment/assistant/create-assistant')
            }
          />
        </div>
      )}

      {/* Pagination */}
      {assistantAction.assistants && assistantAction.assistants.length > 0 && (
        <Pagination
          className="shrink-0"
          totalItems={assistantAction.totalCount}
          page={assistantAction.page}
          pageSize={assistantAction.pageSize}
          pageSizes={[10, 20, 50]}
          onChange={({ page, pageSize }) => {
            if (pageSize !== assistantAction.pageSize) {
              assistantAction.setPageSize(pageSize);
            } else {
              assistantAction.setPage(page);
            }
          }}
        />
      )}
    </div>
  );
}
