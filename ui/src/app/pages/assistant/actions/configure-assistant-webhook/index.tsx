import React, { FC, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { toHumanReadableDateTime } from '@/utils/date';
import { useCurrentCredential } from '@/hooks/use-credential';
import { useRapidaStore } from '@/hooks';
import { SectionLoader } from '@/app/components/loader/section-loader';
import { CreateAssistantWebhook } from './create-assistant-webhook';
import toast from 'react-hot-toast/headless';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { UpdateAssistantWebhook } from '@/app/pages/assistant/actions/configure-assistant-webhook/update-assistant-webhook';
import { useAssistantWebhookPageStore } from '@/app/pages/assistant/actions/store/use-webhook-page-store';
import { CarbonStatusIndicator } from '@/app/components/carbon/status-indicator';
import {
  OverflowMenu,
  OverflowMenuItem,
} from '@/app/components/carbon/overflow-menu';
import { PrimaryButton } from '@/app/components/carbon/button';
import { Pagination } from '@/app/components/carbon/pagination';
import { Add, Renew } from '@carbon/icons-react';
import { Tag } from '@carbon/react';
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
  Button,
} from '@carbon/react';
import { TableSection } from '@/app/components/sections/table-section';

export function ConfigureAssistantWebhookPage() {
  const { assistantId } = useParams();
  return (
    <>
      {assistantId && <ConfigureAssistantWebhook assistantId={assistantId} />}
    </>
  );
}

export function CreateAssistantWebhookPage() {
  const { assistantId } = useParams();
  return (
    <>{assistantId && <CreateAssistantWebhook assistantId={assistantId} />}</>
  );
}

export function UpdateAssistantWebhookPage() {
  const { assistantId } = useParams();
  return (
    <>{assistantId && <UpdateAssistantWebhook assistantId={assistantId} />}</>
  );
}

const headers = [
  { key: 'httpUrl', header: 'Endpoint' },
  { key: 'events', header: 'Events' },
  { key: 'maxRetryCount', header: 'Retries' },
  { key: 'timeoutSeconds', header: 'Timeout (s)' },
  { key: 'executionPriority', header: 'Priority' },
  { key: 'status', header: 'Status' },
  { key: 'created_date', header: 'Created' },
  { key: 'actions', header: '' },
];

const ConfigureAssistantWebhook: FC<{ assistantId: string }> = ({
  assistantId,
}) => {
  const navigation = useGlobalNavigation();
  const axtion = useAssistantWebhookPageStore();
  const { authId, token, projectId } = useCurrentCredential();
  const { loading, showLoader, hideLoader } = useRapidaStore();

  useEffect(() => {
    showLoader('block');
    get();
  }, []);

  const get = () => {
    axtion.getAssistantWebhook(
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

  const deleteAssistantWebhook = (assistantId: string, webhookId: string) => {
    showLoader('block');
    axtion.deleteAssistantWebhook(
      assistantId,
      webhookId,
      projectId,
      token,
      authId,
      e => {
        toast.error(e);
        hideLoader();
      },
      v => {
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
    <div className="h-full flex flex-col flex-1">
      <TableToolbar>
        <TableToolbarContent>
          <TableToolbarSearch placeholder="Search webhooks..." />
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
            onClick={() => navigation.goToCreateAssistantWebhook(assistantId)}
          >
            Create new webhook
          </PrimaryButton>
        </TableToolbarContent>
      </TableToolbar>
      <TableSection>
        {axtion.webhooks.length > 0 ? (
          <>
            <Table>
              <TableHead>
                <TableRow>
                  {headers.map(h => (
                    <TableHeader key={h.key}>{h.header}</TableHeader>
                  ))}
                </TableRow>
              </TableHead>
              <TableBody>
                {axtion.webhooks.map(row => (
                  <TableRow key={row.getId()}>
                    <TableCell>
                      <span className="font-mono text-xs">
                        {row.getHttpmethod()}
                      </span>{' '}
                      <span className="truncate">{row.getHttpurl()}</span>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {row.getAssistanteventsList().map((event, index) => (
                          <Tag key={index} type="blue" size="sm">
                            {event}
                          </Tag>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>{row.getRetrycount()}</TableCell>
                    <TableCell>{row.getTimeoutsecond()}</TableCell>
                    <TableCell>{row.getExecutionpriority()}</TableCell>
                    <TableCell>
                      <CarbonStatusIndicator state={row.getStatus()} />
                    </TableCell>
                    <TableCell>
                      {row.getCreateddate() &&
                        toHumanReadableDateTime(row.getCreateddate()!)}
                    </TableCell>
                    <TableCell>
                      <OverflowMenu size="sm" flipped>
                        <OverflowMenuItem
                          itemText="Update webhook"
                          onClick={() =>
                            navigation.goToEditAssistantWebhook(
                              assistantId,
                              row.getId(),
                            )
                          }
                        />
                        <OverflowMenuItem
                          itemText="Delete webhook"
                          isDelete
                          onClick={() =>
                            deleteAssistantWebhook(assistantId, row.getId())
                          }
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
              onChange={({ page: newPage, pageSize: newSize }) => {
                axtion.setPage(newPage);
                if (newSize !== axtion.pageSize) axtion.setPageSize(newSize);
              }}
            />
          </>
        ) : (
          <EmptyState
            className="w-full"
            title="No Webhook"
            subtitle="There are no assistant webhook found."
            action="Create new webhook"
            onAction={() =>
              navigation.goToCreateAssistantWebhook(assistantId)
            }
          />
        )}
      </TableSection>
    </div>
  );
};
