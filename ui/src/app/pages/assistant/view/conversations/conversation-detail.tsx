import { useEffect, useState } from 'react';
import {
  AssistantChatContext,
  useAssistantChat,
} from '@/hooks/use-assistant-chat';
import { ConversationMessages } from '@/app/pages/assistant/view/conversations/conversation-messages';
import {
  ConnectionConfig,
  FieldSelector,
  GetAssistantConversation,
  GetAssistantConversationRequest,
} from '@rapidaai/react';
import { useParams } from 'react-router-dom';
import { useCurrentCredential } from '@/hooks/use-credential';
import { AssistantConversation } from '@rapidaai/react';
import { useRapidaStore } from '@/hooks';
import { PageLoader } from '@/app/components/loader/page-loader';
import { ArrowLeft, Renew } from '@carbon/icons-react';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { GhostButton } from '@/app/components/carbon/button';
import { PageHeaderBlock } from '@/app/components/blocks/page-header-block';
import { Tab } from '@/app/components/tab-link';
import { Table } from '@/app/components/base/tables/table';
import { TableHead } from '@/app/components/base/tables/table-head';
import { TableBody } from '@/app/components/base/tables/table-body';
import { TableRow } from '@/app/components/base/tables/table-row';
import { TableCell } from '@/app/components/base/tables/table-cell';
import { BlueNoticeBlock } from '@/app/components/container/message/notice-block';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { connectionConfig } from '@/configs';
import { cn } from '@/utils';
import { CarbonStatusIndicator } from '@/app/components/carbon/status-indicator';
import { getStatusMetric } from '@/utils/metadata';

// ── Tab definitions ───────────────────────────────────────────────────────────

const TABS = [
  { key: 'messages', label: 'Messages' },
  { key: 'context', label: 'Context' },
  { key: 'arguments', label: 'Arguments' },
  { key: 'analysis', label: 'Analysis' },
  { key: 'metrics', label: 'Metrics' },
] as const;

type TabKey = (typeof TABS)[number]['key'];

// ── Page component ────────────────────────────────────────────────────────────

export function ConversationDetailPage() {
  const { assistantId, sessionId } = useParams();
  const { authId, token, projectId } = useCurrentCredential();
  const { loading, showLoader, hideLoader } = useRapidaStore();
  const actions = useAssistantChat();
  const navigator = useGlobalNavigation();
  const [currentConversation, setCurrentConversation] =
    useState<AssistantConversation | null>(null);
  const [activeTab, setActiveTab] = useState<TabKey>('messages');

  const get = () => {
    showLoader();
    const request = new GetAssistantConversationRequest();
    request.setAssistantid(assistantId!);
    request.setId(sessionId!);
    const filed = new FieldSelector();
    filed.setField('recording');
    request.addSelectors(filed);
    GetAssistantConversation(
      connectionConfig,
      request,
      ConnectionConfig.WithDebugger({
        authorization: token,
        userId: authId,
        projectId: projectId,
      }),
    )
      .then(response => {
        hideLoader();
        if (response?.getSuccess() && response.getData()) {
          setCurrentConversation(response.getData()!);
        }
      })
      .catch(() => {
        hideLoader();
      });
  };

  useEffect(() => {
    if (!assistantId || !sessionId) return;
    get();
  }, [assistantId, sessionId]);

  if (loading || currentConversation == null) {
    return <PageLoader />;
  }

  const renderContent = () => {
    switch (activeTab) {
      case 'analysis': {
        const items = currentConversation
          .getMetadataList()
          .filter(x => x.getKey().startsWith('analysis.'));
        return items.length === 0 ? (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState
              title="No Analysis"
              subtitle="There is no analysis yet for this conversation"
            />
          </div>
        ) : (
          <div className="flex flex-col divide-y divide-gray-200 dark:divide-gray-800">
            {items.map((x, idx) => (
              <div
                key={idx}
                className="px-6 py-4 space-y-3 bg-white dark:bg-gray-900"
              >
                <p className="text-xs font-medium uppercase tracking-[0.08em] text-gray-500 dark:text-gray-400">
                  {x.getKey().replace('.', ' › ')}
                </p>
                <JsonViewer data={x.getValue()} />
              </div>
            ))}
          </div>
        );
      }

      case 'context': {
        const contexts = currentConversation.getContextsList();
        return contexts.length === 0 ? (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState
              title="No Context"
              subtitle="No knowledge context was retrieved for this conversation"
            />
          </div>
        ) : (
          <div className="flex flex-col divide-y divide-gray-200 dark:divide-gray-800">
            {contexts.map((x, idx) => (
              <div key={idx} className="px-6 py-5 space-y-4">
                <DataField
                  label="Query"
                  value={x
                    .getQuery()
                    ?.getFieldsMap()
                    .get('query')
                    ?.getStringValue()}
                />
                <DataField
                  label="Additional Filter"
                  value={JSON.stringify(
                    x
                      .getQuery()
                      ?.getFieldsMap()
                      .get('additionalData')
                      ?.getStructValue()
                      ?.toJavaScript(),
                  )}
                />
                <DataField
                  label={`Content${
                    x
                      .getResult()
                      ?.getFieldsMap()
                      .get('score')
                      ?.getNumberValue() != null
                      ? ` · score ${x.getResult()?.getFieldsMap().get('score')?.getNumberValue()}`
                      : ''
                  }`}
                  value={x
                    .getResult()
                    ?.getFieldsMap()
                    .get('content')
                    ?.getStringValue()}
                />
                <DataField
                  label="Document"
                  value={JSON.stringify(
                    x.getMetadata()?.toJavaScript(),
                    null,
                    2,
                  )}
                  mono
                />
              </div>
            ))}
          </div>
        );
      }

      case 'messages':
        return (
          <AssistantChatContext.Provider value={actions}>
            <ConversationMessages
              conversation={currentConversation}
              assistantId={currentConversation.getAssistantid()}
              conversationId={currentConversation.getId()}
            />
          </AssistantChatContext.Provider>
        );

      case 'metrics':
        return currentConversation.getMetricsList().length > 0 ? (
          <Table className="w-full bg-white dark:bg-gray-900">
            <TableHead
              columns={[
                { name: 'Name', key: 'Name' },
                { name: 'Value', key: 'Value' },
                { name: 'Description', key: 'Description' },
              ]}
            />
            <TableBody>
              {currentConversation.getMetricsList().map((m, i) => (
                <TableRow key={i}>
                  <TableCell>{m.getName()}</TableCell>
                  <TableCell className="break-words break-all">
                    {m.getValue()}
                  </TableCell>
                  <TableCell className="break-words break-all">
                    {m.getDescription()}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <BlueNoticeBlock>
            No metrics have been captured for this conversation.
          </BlueNoticeBlock>
        );

      case 'arguments':
        return (
          <Table className="bg-white dark:bg-gray-900">
            <TableHead
              columns={[
                { name: 'Type', key: 'Type' },
                { name: 'Name', key: 'Name' },
                { name: 'Value', key: 'Value' },
              ]}
            />
            <TableBody>
              {currentConversation.getArgumentsList().map((m, i) => (
                <TableRow key={`arg-${i}`}>
                  <TableCell>Argument</TableCell>
                  <TableCell>{m.getName()}</TableCell>
                  <TableCell>{m.getValue()}</TableCell>
                </TableRow>
              ))}
              {currentConversation.getOptionsList().map((m, i) => (
                <TableRow key={`opt-${i}`}>
                  <TableCell>Option</TableCell>
                  <TableCell>{m.getKey()}</TableCell>
                  <TableCell>{m.getValue()}</TableCell>
                </TableRow>
              ))}
              {currentConversation.getMetadataList().map((m, i) => (
                <TableRow key={`meta-${i}`}>
                  <TableCell>Metadata</TableCell>
                  <TableCell>{m.getKey()}</TableCell>
                  <TableCell className="break-words break-all">
                    {m.getValue()}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        );
    }
  };

  return (
    <>
      {/* ── Page header: breadcrumb + status + refresh ── */}
      <PageHeaderBlock>
        <div className="flex items-center gap-1.5 min-w-0">
          <GhostButton
            size="sm"
            hasIconOnly
            renderIcon={ArrowLeft}
            iconDescription="Back to sessions"
            onClick={() => navigator.goToAssistantSessionList(assistantId!)}
          />
          <span className="text-sm font-medium text-gray-500 dark:text-gray-400">
            Sessions
          </span>
          <span className="text-gray-300 dark:text-gray-600">/</span>
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100 font-mono truncate">
            {sessionId}
          </span>
        </div>
        <div className="flex items-center gap-2 px-2">
          <CarbonStatusIndicator
            state={getStatusMetric(currentConversation.getMetricsList())}
          />
          <div className="w-px h-4 bg-gray-200 dark:bg-gray-700" />
          <GhostButton
            size="sm"
            hasIconOnly
            renderIcon={Renew}
            iconDescription="Refresh"
            onClick={get}
          />
        </div>
      </PageHeaderBlock>

      {/* ── Horizontal tab bar ── */}
      <div className="flex items-stretch border-b border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 shrink-0 overflow-x-auto">
        {TABS.map(({ key, label }) => (
          <Tab
            key={key}
            isActive={activeTab === key}
            onClick={() => setActiveTab(key)}
          >
            {label}
          </Tab>
        ))}
      </div>

      {/* ── Content area ── */}
      <div className="flex-1 overflow-auto flex flex-col">
        {renderContent()}
      </div>
    </>
  );
}

// ── Helper components ─────────────────────────────────────────────────────────

const DataField = ({
  label,
  value,
  mono = false,
}: {
  label: string;
  value?: string | null;
  mono?: boolean;
}) => (
  <div>
    <p className="text-xs font-medium uppercase tracking-[0.08em] text-gray-500 dark:text-gray-400 mb-1">
      {label}
    </p>
    <p
      className={cn(
        'text-sm text-gray-900 dark:text-gray-100 leading-relaxed',
        mono && 'font-mono',
      )}
    >
      {value ?? '—'}
    </p>
  </div>
);

interface JsonViewerProps {
  data: string | any;
  preview?: boolean;
}

const JsonViewer = ({ data, preview = false }: JsonViewerProps) => {
  const parsedData = typeof data === 'string' ? JSON.parse(data) : data;

  const isSimpleResult =
    parsedData &&
    typeof parsedData === 'object' &&
    parsedData.result &&
    Object.keys(parsedData).length === 1;

  const extractInsights = (
    obj: any,
  ): { key: string; value: any; type: string }[] => {
    const insights: { key: string; value: any; type: string }[] = [];

    const processObject = (object: any, prefix = '') => {
      if (typeof object === 'object' && object !== null) {
        Object.entries(object).forEach(([key, value]) => {
          const formattedKey = prefix ? `${prefix}.${key}` : key;
          if (
            typeof value === 'object' &&
            value !== null &&
            !Array.isArray(value)
          ) {
            processObject(value, formattedKey);
          } else {
            insights.push({
              key: formattedKey.replace(/_/g, ' ').replace(/\./g, ' › '),
              value,
              type: Array.isArray(value) ? 'array' : typeof value,
            });
          }
        });
      }
    };

    processObject(obj);
    return insights;
  };

  const insights = extractInsights(parsedData);
  const displayInsights =
    preview && insights.length > 3 ? insights.slice(0, 3) : insights;

  const formatValue = (value: any, type: string) => {
    if (type === 'array')
      return Array.isArray(value) ? value.join(', ') : String(value);
    if (type === 'number')
      return typeof value === 'number'
        ? `${Math.round(value * 100)}%`
        : String(value);
    return String(value);
  };

  if (isSimpleResult) {
    return (
      <p className="text-sm text-gray-900 dark:text-gray-100 leading-relaxed">
        {parsedData.result}
      </p>
    );
  }

  return (
    <div className="space-y-2">
      {displayInsights.map((insight, index) => (
        <div
          key={index}
          className="border-l-2 border-primary bg-primary/5 pl-4 py-2"
        >
          <p className="text-xs font-medium uppercase tracking-[0.08em] text-gray-500 dark:text-gray-400 mb-1 capitalize">
            {insight.key}
          </p>
          <p className="text-sm text-gray-900 dark:text-gray-100 leading-relaxed">
            {formatValue(insight.value, insight.type)}
          </p>
        </div>
      ))}
    </div>
  );
};

export default JsonViewer;
