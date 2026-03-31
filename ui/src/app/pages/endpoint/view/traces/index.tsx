import { useState, useEffect, FC } from 'react';
import { Helmet } from '@/app/components/helmet';
import { useCredential } from '@/hooks/use-credential';
import toast from 'react-hot-toast/headless';
import { useRapidaStore } from '@/hooks';
import { Endpoint, EndpointLog } from '@rapidaai/react';
import { SourceIndicator } from '@/app/components/indicators/source';
import { toDate, toDateString } from '@/utils/date';
import { getTimeTakenMetric, getTotalTokenMetric } from '@/utils/metadata';
import { EndpointTraceModal } from '@/app/components/base/modal/endpoint-trace-modal';
import { useEndpointLogPage } from '@/hooks/use-endpoint-log-page-store';
import { CarbonStatusIndicator } from '@/app/components/carbon/status-indicator';
import { Pagination } from '@/app/components/carbon/pagination';
import { IconOnlyButton } from '@/app/components/carbon/button';
import { DateFilter } from '@/app/components/carbon/date-filter';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { Renew, View, Activity } from '@carbon/icons-react';

import {
  Table,
  TableHead,
  TableRow,
  TableHeader,
  TableBody,
  TableCell,
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
  Loading,
  DefinitionTooltip,
  Tag,
} from '@carbon/react';

export const EndpointTraces: FC<{ currentEndpoint: Endpoint }> = props => {
  const { loading, showLoader, hideLoader } = useRapidaStore();
  const [userId, token, projectId] = useCredential();
  const [currentTrace, setCurrentTrace] = useState<EndpointLog | null>(null);
  const [showTraceModal, setShowTraceModal] = useState(false);

  const {
    getLogs,
    addCriterias,
    endpointLogs,
    onChangeLogs,
    columns,
    page,
    setPage,
    totalCount,
    criteria,
    pageSize,
    setPageSize,
    visibleColumn,
    setColumns,
  } = useEndpointLogPage();

  const onDateSelect = (to: Date, from: Date) => {
    addCriterias([
      { k: 'created_date', v: toDateString(to), logic: '<=' },
      { k: 'created_date', v: toDateString(from), logic: '>=' },
    ]);
  };

  useEffect(() => {
    onGetAllEndpointLogs();
  }, [
    projectId,
    page,
    pageSize,
    JSON.stringify(criteria),
    props.currentEndpoint.getId(),
  ]);

  const onGetAllEndpointLogs = () => {
    showLoader();
    getLogs(
      props.currentEndpoint.getId(),
      projectId,
      token,
      userId,
      err => {
        hideLoader();
        toast.error(err);
      },
      logs => {
        hideLoader();
        onChangeLogs(logs);
      },
    );
  };

  const visibleColumns = columns.filter(c => c.visible);

  return (
    <div className="flex flex-1 flex-col">
      <Helmet title="Endpoint Logs" />

      <EndpointTraceModal
        modalOpen={showTraceModal}
        setModalOpen={setShowTraceModal}
        currentTrace={currentTrace}
      />

      <TableToolbar>
        <TableToolbarContent>
          <TableToolbarSearch placeholder="Search endpoint traces" />
          <DateFilter
            onApply={(from, to) => onDateSelect(to, from)}
            onReset={() => addCriterias([])}
          />
          <IconOnlyButton
            kind="ghost"
            size="lg"
            renderIcon={Renew}
            iconDescription="Refresh"
            onClick={() => onGetAllEndpointLogs()}
          />
        </TableToolbarContent>
      </TableToolbar>

      {loading ? (
        <div className="flex items-center justify-center py-16">
          <Loading withOverlay={false} small />
        </div>
      ) : endpointLogs && endpointLogs.length > 0 ? (
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
              {endpointLogs.map((row, idx) => (
                <TableRow key={idx}>
                  {visibleColumn('id') && (
                    <TableCell className="!font-mono !text-xs">{row.getId()}</TableCell>
                  )}
                  {visibleColumn('version') && (
                    <TableCell>
                      <Tag size="md" type="cool-gray">
                        <span className="font-mono leading-none">vrsn_{row.getEndpointprovidermodelid()}</span>
                      </Tag>
                    </TableCell>
                  )}
                  {visibleColumn('source') && (
                    <TableCell>
                      <SourceIndicator source={row.getSource()} />
                    </TableCell>
                  )}
                  {visibleColumn('status') && (
                    <TableCell>
                      <CarbonStatusIndicator state={row.getStatus()} />
                    </TableCell>
                  )}
                  {visibleColumn('action') && (
                    <TableCell>
                      <IconOnlyButton
                        kind="ghost"
                        size="md"
                        renderIcon={View}
                        iconDescription="View detail"
                        onClick={() => {
                          setCurrentTrace(row);
                          setShowTraceModal(true);
                        }}
                      />
                    </TableCell>
                  )}
                  {visibleColumn('timetaken') && (
                    <TableCell className="!font-mono !text-xs tabular-nums">
                      {Number(row.getTimetaken()) / 1000000}ms
                    </TableCell>
                  )}
                  {visibleColumn('total_token') && (
                    <TableCell className="tabular-nums">
                      {getTotalTokenMetric(row.getMetricsList())}
                    </TableCell>
                  )}
                  {visibleColumn('time_taken') && (
                    <TableCell className="!font-mono !text-xs tabular-nums">
                      {getTimeTakenMetric(row.getMetricsList()) / 1000000}ms
                    </TableCell>
                  )}
                  {visibleColumn('created_date') && (
                    <TableCell className="!text-xs whitespace-nowrap">
                      {row.getCreateddate() && (
                        <DefinitionTooltip
                          definition={toDate(row.getCreateddate()!).toUTCString()}
                          openOnHover
                        >
                          {toDate(row.getCreateddate()!).toLocaleString()}
                        </DefinitionTooltip>
                      )}
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        <EmptyState
          icon={Activity}
          title="No endpoint traces found"
          subtitle="API requests made to this endpoint will appear here as traces with latency, token usage, and status details."
        />
      )}

      {endpointLogs && endpointLogs.length > 0 && (
        <Pagination
          className="shrink-0"
          totalItems={totalCount}
          page={page}
          pageSize={pageSize}
          pageSizes={[10, 20, 25, 50, 100]}
          onChange={({ page: p, pageSize: ps }) => {
            if (ps !== pageSize) setPageSize(ps);
            else setPage(p);
          }}
        />
      )}
    </div>
  );
};
