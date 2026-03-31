import { useEffect, useCallback } from 'react';
import { SingleEndpoint } from './single-endpoint';
import { useCredential } from '@/hooks/use-credential';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useEndpointPageStore } from '@/hooks';
import { Helmet } from '@/app/components/helmet';
import { Endpoint } from '@rapidaai/react';
import toast from 'react-hot-toast/headless';
import { useRapidaStore } from '@/hooks';
import { PrimaryButton } from '@/app/components/carbon/button';
import { Pagination } from '@/app/components/carbon/pagination';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { Add, Renew, Connect } from '@carbon/icons-react';
import { PageHeaderBlock } from '@/app/components/blocks/page-header-block';
import { PageTitleBlock } from '@/app/components/blocks/page-title-block';
import {
  Table,
  TableHead,
  TableRow,
  TableHeader,
  TableBody,
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
  Loading,
  Button,
} from '@carbon/react';

export function EndpointPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [userId, token, projectId] = useCredential();
  const endpointActions = useEndpointPageStore();
  const { loading, showLoader, hideLoader } = useRapidaStore();

  useEffect(() => {
    if (searchParams) {
      const searchParamMap = Object.fromEntries(searchParams.entries());
      Object.entries(searchParamMap).forEach(([key, value]) =>
        endpointActions.addCriteria(key, value, '='),
      );
    }
  }, [searchParams]);

  const onError = useCallback((err: string) => {
    hideLoader();
    toast.error(err);
  }, []);

  const onSuccess = useCallback((data: Endpoint[]) => {
    hideLoader();
  }, []);

  const getEndpoints = useCallback((projectId, token, userId) => {
    showLoader();
    endpointActions.onGetAllEndpoint(
      projectId,
      token,
      userId,
      onError,
      onSuccess,
    );
  }, []);

  useEffect(() => {
    getEndpoints(projectId, token, userId);
  }, [
    projectId,
    endpointActions.page,
    endpointActions.pageSize,
    endpointActions.criteria,
  ]);

  const visibleColumns = endpointActions.columns.filter(x => x.visible);

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <Helmet title="Hosted endpoints" />

      <PageHeaderBlock>
        <div className="flex items-center gap-3">
          <PageTitleBlock>Hosted Endpoints</PageTitleBlock>
          <span className="text-xs text-gray-500 dark:text-gray-400 tabular-nums">
            {endpointActions.endpoints.length}/{endpointActions.totalCount}
          </span>
        </div>
      </PageHeaderBlock>

      <TableToolbar>
        <TableToolbarContent>
          <TableToolbarSearch placeholder="Search endpoints..." />
          <Button
            hasIconOnly
            renderIcon={Renew}
            iconDescription="Refresh"
            kind="ghost"
            onClick={() => getEndpoints(projectId, token, userId)}
            tooltipPosition="bottom"
          />
          <PrimaryButton
            size="md"
            renderIcon={Add}
            isLoading={loading}
            onClick={() => navigate('/deployment/endpoint/create-endpoint')}
          >
            Add new endpoint
          </PrimaryButton>
        </TableToolbarContent>
      </TableToolbar>

      {loading ? (
        <div className="flex items-center justify-center py-16">
          <Loading withOverlay={false} small />
        </div>
      ) : endpointActions.endpoints && endpointActions.endpoints.length > 0 ? (
        <div className="overflow-auto flex-1">
          <Table>
            <TableHead>
              <TableRow>
                {visibleColumns.map(col => (
                  <TableHeader key={col.key}>{col.name}</TableHeader>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {endpointActions.endpoints.map(ed => (
                <SingleEndpoint
                  key={`endpoint_row_${ed.getId()}`}
                  endpoint={ed}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        <EmptyState
          icon={Connect}
          title="No endpoints found"
          subtitle="Deploy API endpoints with fine-grained governance, audit trails, and enterprise access control."
          action="Create new endpoint"
          onAction={() => navigate('/deployment/endpoint/create-endpoint')}
        />
      )}

      {endpointActions.endpoints && endpointActions.endpoints.length > 0 && (
        <Pagination
          className="shrink-0"
          totalItems={endpointActions.totalCount}
          page={endpointActions.page}
          pageSize={endpointActions.pageSize}
          pageSizes={[10, 20, 50]}
          onChange={({ page, pageSize }) => {
            if (pageSize !== endpointActions.pageSize) {
              endpointActions.setPageSize(pageSize);
            } else {
              endpointActions.setPage(page);
            }
          }}
        />
      )}
    </div>
  );
}
