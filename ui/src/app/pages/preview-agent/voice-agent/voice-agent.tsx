import React, { FC, memo, useEffect, useMemo, useRef, useState } from 'react';
import {
  VoiceAgent as VI,
  ConnectionConfig,
  AgentConfig,
  AgentCallback,
  Assistant,
  Variable,
  ConversationError,
} from '@rapidaai/react';
import { MessagingAction } from '@/app/pages/preview-agent/voice-agent/actions';
import { ConversationMessages } from '@/app/pages/preview-agent/voice-agent/text/conversations';
import { cn } from '@/utils';
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';
import {
  JsonTextarea,
  NumberTextarea,
  ParagraphTextarea,
  TextTextarea,
  UrlTextarea,
} from '@/app/components/form/textarea';
import { InputVarForm } from '@/app/pages/endpoint/view/try-playground/experiment-prompt/components/input-var-form';
import { InputVarType } from '@/models/common';
import { useRapidaStore } from '@/hooks';
import { PageLoader } from '@/app/components/loader/page-loader';
import { Notification } from '@/app/components/carbon/notification';
import {
  YellowNoticeBlock,
} from '@/app/components/container/message/notice-block';
import { GhostButton, IconOnlyButton } from '@/app/components/carbon/button';
import {
  Tabs,
  TabList,
  Tab,
  DismissibleTag,
  Tag,
  StructuredListWrapper,
  StructuredListBody,
  StructuredListRow,
  StructuredListCell,
} from '@carbon/react';
import { ArrowLeft } from '@carbon/icons-react';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type EventEntry = {
  type:
    | 'directive'
    | 'configuration'
    | 'userMessage'
    | 'assistantMessage'
    | 'interrupt'
    | 'pipelineEvent'
    | 'metric';
  ts: Date;
  payload: any;
};

type MsgTab = 'messages' | 'events';

/** Returns the display label for an event — matches the 2nd column in the events table. */
function getEventLabel(entry: EventEntry): string {
  if (entry.type === 'pipelineEvent') return entry.payload?.name ?? 'pipeline';
  if (entry.type === 'userMessage') return 'user';
  if (entry.type === 'assistantMessage') return 'assistant';
  if (entry.type === 'configuration') return 'session';
  if (entry.type === 'interrupt') return 'interrupt';
  if (entry.type === 'metric') return 'metric';
  return entry.type;
}

// ---------------------------------------------------------------------------
// Conversation event row
// ---------------------------------------------------------------------------

const EVENT_COLORS: Record<string, string> = {
  session: 'text-gray-500 dark:text-gray-400',
  stt: 'text-green-600 dark:text-green-400',
  llm: 'text-blue-600 dark:text-blue-400',
  tts: 'text-violet-600 dark:text-violet-400',
  vad: 'text-yellow-600 dark:text-yellow-400',
  eos: 'text-cyan-600 dark:text-cyan-400',
  denoise: 'text-orange-600 dark:text-orange-400',
  audio: 'text-slate-600 dark:text-slate-400',
  tool: 'text-pink-600 dark:text-pink-400',
  behavior: 'text-rose-600 dark:text-rose-400',
  knowledge: 'text-teal-600 dark:text-teal-400',
};

const ConversationEventRow: FC<{ entry: EventEntry }> = ({ entry }) => {
  const [expanded, setExpanded] = useState(false);
  const ts = entry.ts.toISOString().slice(11, 23);
  const toggle = () => setExpanded(p => !p);

  if (entry.type === 'pipelineEvent') {
    const { name, dataMap, id, time } = entry.payload as {
      name: string;
      dataMap: Array<[string, string]>;
      id?: string;
      time?: unknown;
    };
    const data = Object.fromEntries(dataMap ?? []);
    const color = EVENT_COLORS[name] ?? 'text-gray-500 dark:text-gray-400';
    const jsonPayload = { id, time, ...data };

    return (
      <>
        <tr
          className="hover:bg-gray-100 dark:hover:bg-gray-800/60 cursor-pointer select-text"
          onClick={toggle}
        >
          <td className="pl-3 pr-2 py-[3px] whitespace-nowrap tabular-nums text-gray-400 dark:text-gray-500">
            {ts}
          </td>
          <td
            className={cn(
              'px-2 py-[3px] whitespace-nowrap font-semibold',
              color,
            )}
          >
            {name}
          </td>
          <td
            colSpan={2}
            className="px-2 pr-3 py-[3px] text-gray-600 dark:text-gray-300 max-w-0 overflow-hidden truncate"
          >
            {JSON.stringify(jsonPayload)}
          </td>
        </tr>
        {expanded && (
          <tr className="bg-gray-50 dark:bg-gray-800/40">
            <td />
            <td colSpan={3} className="pl-2 pr-3 pt-1 pb-2">
              <pre className="whitespace-pre-wrap break-all text-gray-700 dark:text-gray-200 text-sm/6">
                {JSON.stringify(jsonPayload, null, 2)}
              </pre>
            </td>
          </tr>
        )}
      </>
    );
  }

  // Non-pipeline events — time | role | json
  const label =
    entry.type === 'userMessage'
      ? 'user'
      : entry.type === 'assistantMessage'
        ? 'assistant'
        : entry.type === 'configuration'
          ? 'session'
          : entry.type === 'interrupt'
            ? 'interrupt'
            : entry.type === 'metric'
              ? 'metric'
              : entry.type;

  const labelColor =
    entry.type === 'userMessage'
      ? 'text-emerald-600 dark:text-emerald-400'
      : entry.type === 'assistantMessage'
        ? 'text-indigo-600 dark:text-indigo-400'
        : entry.type === 'interrupt'
          ? 'text-orange-600 dark:text-orange-400'
          : entry.type === 'configuration'
            ? 'text-sky-600 dark:text-sky-400'
            : entry.type === 'metric'
              ? 'text-lime-600 dark:text-lime-400'
              : 'text-red-600 dark:text-red-400';

  return (
    <>
      <tr
        className="hover:bg-gray-100 dark:hover:bg-gray-800/60 cursor-pointer select-text"
        onClick={toggle}
      >
        <td className="pl-3 pr-2 py-[3px] whitespace-nowrap tabular-nums text-gray-400 dark:text-gray-500">
          {ts}
        </td>
        <td
          className={cn(
            'px-2 py-[3px] whitespace-nowrap font-semibold',
            labelColor,
          )}
        >
          {label}
        </td>
        <td
          colSpan={2}
          className="px-2 pr-3 py-[3px] text-gray-600 dark:text-gray-300 max-w-0 overflow-hidden truncate"
        >
          {JSON.stringify(entry.payload)}
        </td>
      </tr>
      {expanded && (
        <tr className="bg-gray-50 dark:bg-gray-800/40">
          <td />
          <td colSpan={3} className="pl-2 pr-3 pt-1 pb-2">
            <pre className="whitespace-pre-wrap break-all text-gray-700 dark:text-gray-200 text-sm/6">
              {JSON.stringify(entry.payload, null, 2)}
            </pre>
          </td>
        </tr>
      )}
    </>
  );
};

// ---------------------------------------------------------------------------
// Main layout
// ---------------------------------------------------------------------------

export const VoiceAgent: FC<{
  debug: boolean;
  connectConfig: ConnectionConfig;
  agentConfig: AgentConfig;
  agentCallback?: AgentCallback;
}> = ({ debug, connectConfig, agentConfig, agentCallback }) => {
  const voiceAgentContextValue = React.useMemo(
    () => new VI(connectConfig, agentConfig, agentCallback),
    [connectConfig, agentConfig, agentCallback],
  );

  const [assistant, setAssistant] = useState<Assistant | null>(null);
  const [events, setEvents] = useState<EventEntry[]>([]);
  const [variables, setVariables] = useState<Variable[]>([]);
  const [msgTab, setMsgTab] = useState<MsgTab>('messages');
  const [eventFilters, setEventFilters] = useState<Set<string>>(new Set());
  const [conversationError, setConversationError] =
    useState<ConversationError.AsObject | null>(null);
  const callbackRegistered = useRef(false);
  const eventsBottomRef = useRef<HTMLDivElement>(null);

  const { loading, showLoader, hideLoader } = useRapidaStore();

  // Fetch assistant info
  useEffect(() => {
    showLoader('block');
    new VI(connectConfig, agentConfig, agentCallback)
      .getAssistant()
      .then(ex => {
        hideLoader();
        if (ex.getSuccess()) setAssistant(ex.getData()!);
      })
      .catch(() => hideLoader());
  }, []);

  // Load variables from assistant
  useEffect(() => {
    if (!assistant) return;
    const pmtVar = assistant
      .getAssistantprovidermodel()
      ?.getTemplate()
      ?.getPromptvariablesList();
    if (pmtVar) {
      pmtVar.forEach(v => {
        if (v.getDefaultvalue())
          voiceAgentContextValue.agentConfiguration.addArgument(
            v.getName(),
            v.getDefaultvalue(),
          );
      });
      setVariables(pmtVar);
    }
  }, [assistant]);

  // Register callbacks once
  useEffect(() => {
    if (callbackRegistered.current) return;
    callbackRegistered.current = true;
    voiceAgentContextValue.registerCallback({
      onDirective: arg =>
        setEvents(p => [
          ...p,
          { type: 'directive', ts: new Date(), payload: arg },
        ]),
      onConfiguration: args =>
        setEvents(p => [
          ...p,
          { type: 'configuration', ts: new Date(), payload: args },
        ]),
      onUserMessage: args =>
        setEvents(p => [
          ...p,
          { type: 'userMessage', ts: new Date(), payload: args },
        ]),
      onAssistantMessage: args => {
        if (args?.messageText)
          setEvents(p => [
            ...p,
            { type: 'assistantMessage', ts: new Date(), payload: args },
          ]);
      },
      onInterrupt: args =>
        setEvents(p => [
          ...p,
          { type: 'interrupt', ts: new Date(), payload: args },
        ]),
      onConversationEvent: event =>
        setEvents(p => [
          ...p,
          { type: 'pipelineEvent', ts: new Date(), payload: event },
        ]),
      onMetric: metric =>
        setEvents(p => [
          ...p,
          { type: 'metric', ts: new Date(), payload: metric },
        ]),
      onConversationError: error => setConversationError(error),
    });
  }, [voiceAgentContextValue]);

  // Auto-scroll events tab when new events arrive
  useEffect(() => {
    if (msgTab === 'events') {
      setTimeout(
        () => eventsBottomRef.current?.scrollIntoView({ behavior: 'smooth' }),
        50,
      );
    }
  }, [events.length, msgTab]);

  // Derive unique labels from events for the filter bar
  const availableEventLabels = useMemo(() => {
    const labels = new Set<string>();
    events.forEach(e => labels.add(getEventLabel(e)));
    return Array.from(labels);
  }, [events]);

  // Filter events — empty set means show all
  const filteredEvents = useMemo(() => {
    if (eventFilters.size === 0) return events;
    return events.filter(e => eventFilters.has(getEventLabel(e)));
  }, [events, eventFilters]);

  const toggleEventFilter = (label: string) => {
    setEventFilters(prev => {
      const next = new Set(prev);
      if (next.has(label)) {
        next.delete(label);
      } else {
        next.add(label);
      }
      return next;
    });
  };

  if (loading) return <PageLoader />;

  const voiceWarning = debug
    ? !assistant?.getDebuggerdeployment()?.hasInputaudio()
    : !assistant?.getApideployment()?.hasInputaudio();

  const enableVoiceHref = debug
    ? `/deployment/assistant/${agentConfig.id}/deployment/debugger`
    : `/deployment/assistant/${assistant?.getId()}/manage/deployment/debugger`;

  return (
    <PanelGroup
      className="!h-dvh !overflow-hidden !flex"
      direction="horizontal"
    >
      {/* ── Left: messaging ─────────────────────────────────────────── */}
      <Panel className="flex flex-col h-dvh overflow-hidden w-2/3  bg-white dark:bg-gray-950">
        {/* Header */}
        <div className="shrink-0">
          {debug && (
            <div className="flex items-center gap-1.5 px-3 py-2 border-b border-gray-200 dark:border-gray-800">
              <IconOnlyButton
                kind="ghost"
                size="sm"
                renderIcon={ArrowLeft}
                iconDescription="Back to Assistant"
                onClick={() => {
                  window.location.href = `/deployment/assistant/${agentConfig.id}/overview`;
                }}
              />
              <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
                Back to Assistant
              </span>
            </div>
          )}
          {voiceWarning && (
            <YellowNoticeBlock className="flex items-center justify-between gap-3">
              <span className="text-sm">
                Voice is disabled. Enable it to enjoy a voice experience.
              </span>
              <a
                href={enableVoiceHref}
                target="_blank"
                rel="noreferrer"
                className="shrink-0 text-sm font-medium text-yellow-700 dark:text-yellow-400 hover:underline"
              >
                Enable voice
              </a>
            </YellowNoticeBlock>
          )}
          {conversationError && (
            <Notification
              kind="error"
              title="Error"
              subtitle={
                conversationError.message ||
                'An error occurred during the conversation.'
              }
              hideCloseButton={false}
              onClose={() => setConversationError(null)}
            />
          )}
          {/* Tab bar */}
          <Tabs
            selectedIndex={msgTab === 'messages' ? 0 : 1}
            onChange={({ selectedIndex }) =>
              setMsgTab(selectedIndex === 0 ? 'messages' : 'events')
            }
          >
            <TabList contained aria-label="Message tabs">
              <Tab>Messages</Tab>
              <Tab>
                {events.length > 0 ? `Events (${events.length})` : 'Events'}
              </Tab>
            </TabList>
          </Tabs>
        </div>

        {/* Messages tab */}
        {msgTab === 'messages' &&
          (() => {
            const hasMessages = events.some(
              e => e.type === 'userMessage' || e.type === 'assistantMessage',
            );
            return hasMessages ? (
              <div className="flex flex-col grow min-h-0 overflow-y-auto px-4 py-4">
                <ConversationMessages vag={voiceAgentContextValue} />
              </div>
            ) : (
              <AssistantPlaceholder assistant={assistant} />
            );
          })()}

        {/* Events tab — structured conversation event rows */}
        {msgTab === 'events' && (
          <div className="flex flex-col flex-1 min-h-0">
            {/* Filter bar */}
            {availableEventLabels.length > 0 && (
              <div className="shrink-0 flex flex-wrap items-center gap-2 px-3 py-2 border-b border-gray-200 dark:border-gray-800">
                <span className="text-xs font-medium text-gray-500 dark:text-gray-400 select-none">
                  Filter
                </span>
                {availableEventLabels.map(label =>
                  eventFilters.has(label) ? (
                    <DismissibleTag
                      key={label}
                      text={label}
                      type="blue"
                      size="md"
                      onClose={() => toggleEventFilter(label)}
                    />
                  ) : (
                    <Tag
                      key={label}
                      type={
                        eventFilters.size === 0 ? 'blue' : 'cool-gray'
                      }
                      size="md"
                      onClick={() => toggleEventFilter(label)}
                      className="cursor-pointer"
                    >
                      {label}
                    </Tag>
                  ),
                )}
                {eventFilters.size > 0 && (
                  <GhostButton
                    size="sm"
                    onClick={() => setEventFilters(new Set())}
                  >
                    Clear all
                  </GhostButton>
                )}
              </div>
            )}

            <div className="flex-1 min-h-0 overflow-y-auto py-1">
              {filteredEvents.length === 0 ? (
                <div className="flex items-center justify-center h-full text-gray-400 dark:text-gray-500 text-sm/6 font-mono">
                  {events.length === 0
                    ? 'No events yet…'
                    : 'No events match the selected filters.'}
                </div>
              ) : (
                <table className="w-full table-fixed font-mono text-sm/6 border-collapse">
                  <colgroup>
                    <col className="w-[9rem]" />
                    <col className="w-[6rem]" />
                    <col className="w-[10rem]" />
                    <col />
                  </colgroup>
                  <tbody>
                    {filteredEvents.map((entry, i) => (
                      <ConversationEventRow key={i} entry={entry} />
                    ))}
                  </tbody>
                </table>
              )}
              <div ref={eventsBottomRef} />
            </div>
          </div>
        )}

        {/* Messaging action — always visible */}
        <MessagingAction
          assistant={assistant}
          placeholder="How can I help you?"
          className=" border-t"
          voiceAgent={voiceAgentContextValue}
        />
      </Panel>

      <PanelResizeHandle className="flex w-px! bg-gray-200 dark:bg-gray-800 hover:bg-blue-700 dark:hover:bg-blue-500 items-stretch"></PanelResizeHandle>
      {/* ── Right: assistant + metrics ──────────────────────────────── */}
      <Panel className="shrink-0 flex flex-col overflow-hidden w-1/3 ">
        <VoiceAgentDebugger
          debug={debug}
          voiceAgent={voiceAgentContextValue}
          assistant={assistant}
          variables={variables}
          events={events}
          onChangeArgument={(k, v) =>
            voiceAgentContextValue.agentConfiguration.addArgument(k, v)
          }
        />
      </Panel>
    </PanelGroup>
  );
};

// ---------------------------------------------------------------------------
// Right panel: tabs — arguments | configuration | metrics
// ---------------------------------------------------------------------------

type RightTab = 'arguments' | 'configuration' | 'metrics';

export const VoiceAgentDebugger: FC<{
  debug: boolean;
  voiceAgent: VI;
  assistant: Assistant | null;
  variables: Variable[];
  events: EventEntry[];
  onChangeArgument: (k: string, v: string) => void;
}> = memo(
  ({ debug, voiceAgent, assistant, variables, events, onChangeArgument }) => {
    const RIGHT_TABS: RightTab[] = ['configuration', 'arguments', 'metrics'];
    const [tab, setTab] = useState<RightTab>('configuration');
    const metrics = useMemo(() => computeMetrics(events), [events]);

    const deployment = assistant
      ? (debug
          ? assistant.getDebuggerdeployment()
          : assistant.getApideployment()) ?? null
      : null;
    const stt = deployment?.getInputaudio() ?? null;
    const tts = deployment?.getOutputaudio() ?? null;
    const model = assistant?.getAssistantprovidermodel() ?? null;

    return (
      <div className="flex flex-col h-full overflow-hidden text-sm">
        {/* Tab bar */}
        <Tabs
          selectedIndex={RIGHT_TABS.indexOf(tab)}
          onChange={({ selectedIndex }) => setTab(RIGHT_TABS[selectedIndex])}
        >
          <TabList contained aria-label="Debugger tabs">
            <Tab>Configuration</Tab>
            <Tab>Arguments</Tab>
            <Tab>Metrics</Tab>
          </TabList>
        </Tabs>

        {/* ── arguments tab ── */}
        {tab === 'arguments' && (
          <div className="flex-1 min-h-0 overflow-y-auto">
            {variables.length > 0 ? (
              <div className="divide-y border-b">
                {variables.map((x, idx) => (
                  <InputVarForm key={idx} var={x}>
                    {(x.getType() === InputVarType.stringInput ||
                      x.getType() === InputVarType.textInput) && (
                      <TextTextarea
                        id={x.getName()}
                        defaultValue={x.getDefaultvalue()}
                        onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                          onChangeArgument(x.getName(), e.target.value)
                        }
                      />
                    )}
                    {x.getType() === InputVarType.paragraph && (
                      <ParagraphTextarea
                        id={x.getName()}
                        defaultValue={x.getDefaultvalue()}
                        onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                          onChangeArgument(x.getName(), e.target.value)
                        }
                      />
                    )}
                    {x.getType() === InputVarType.number && (
                      <NumberTextarea
                        id={x.getName()}
                        defaultValue={x.getDefaultvalue()}
                        onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                          onChangeArgument(x.getName(), e.target.value)
                        }
                      />
                    )}
                    {x.getType() === InputVarType.json && (
                      <JsonTextarea
                        id={x.getName()}
                        defaultValue={x.getDefaultvalue()}
                        onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                          onChangeArgument(x.getName(), e.target.value)
                        }
                      />
                    )}
                    {x.getType() === InputVarType.url && (
                      <UrlTextarea
                        id={x.getName()}
                        defaultValue={x.getDefaultvalue()}
                        onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                          onChangeArgument(x.getName(), e.target.value)
                        }
                      />
                    )}
                  </InputVarForm>
                ))}
              </div>
            ) : (
              <p className="p-4 text-sm/6 text-gray-400 dark:text-gray-500">
                No arguments defined.
              </p>
            )}
          </div>
        )}

        {/* ── configuration tab ── */}
        {tab === 'configuration' && (
          <div className="flex-1 min-h-0 overflow-y-auto">
            {assistant ? (
              <>
                <ConfigBlock title="assistant">
                  <InfoRow label="name" value={assistant.getName()} />
                  <InfoRow
                    label="executor"
                    value={
                      assistant.hasAssistantprovideragentkit()
                        ? 'agentkit'
                        : assistant.hasAssistantproviderwebsocket()
                          ? 'websocket'
                          : 'model'
                    }
                  />
                  {assistant.getDescription() && (
                    <InfoRow
                      label="description"
                      value={assistant.getDescription()}
                    />
                  )}
                </ConfigBlock>

                {stt && (
                  <ConfigBlock title="stt">
                    <InfoRow label="provider" value={stt.getAudioprovider()} />
                    {stt.getAudiooptionsList().map(m => (
                      <InfoRow
                        key={m.getKey()}
                        label={m.getKey()}
                        value={m.getValue()}
                      />
                    ))}
                  </ConfigBlock>
                )}

                {tts && (
                  <ConfigBlock title="tts">
                    <InfoRow label="provider" value={tts.getAudioprovider()} />
                    {tts.getAudiooptionsList().map(m => (
                      <InfoRow
                        key={m.getKey()}
                        label={m.getKey()}
                        value={m.getValue()}
                      />
                    ))}
                  </ConfigBlock>
                )}

                {model && (
                  <ConfigBlock title="llm">
                    <InfoRow
                      label="provider"
                      value={model.getModelprovidername()}
                    />
                    {model.getAssistantmodeloptionsList().map(m => (
                      <InfoRow
                        key={m.getKey()}
                        label={m.getKey()}
                        value={m.getValue()}
                      />
                    ))}
                  </ConfigBlock>
                )}
              </>
            ) : (
              <p className="p-4 text-sm/6 text-gray-400 dark:text-gray-500">
                Loading…
              </p>
            )}
          </div>
        )}

        {/* ── metrics tab ── */}
        {tab === 'metrics' && (
          <div className="flex-1 min-h-0 overflow-y-auto p-4">
            {Object.keys(metrics).length === 0 ? (
              <p className="text-sm/6 text-gray-400 dark:text-gray-500">
                No metrics yet.
              </p>
            ) : (
              <div className="grid grid-cols-2 gap-x-6 gap-y-5">
                {Object.entries(metrics).map(([k, v]) => (
                  <MetricCard key={k} label={k} value={String(v)} />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    );
  },
);

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Empty-state placeholder — developer console style
// ---------------------------------------------------------------------------

const AssistantPlaceholder: FC<{
  assistant: Assistant | null;
}> = ({ assistant }) => (
  <div className="flex flex-col flex-1 min-h-0 items-start justify-end gap-1 px-2 pb-6 select-none">
    <span className="text-2xl font-semibold text-gray-800 dark:text-gray-100 italic">
      Hello,
    </span>
    <span className="text-lg text-gray-400 dark:text-gray-500 font-semibold italic">
      How can I help you today?
    </span>
  </div>
);

export const ConfigBlock: FC<{ title: string; children: React.ReactNode }> = ({
  title,
  children,
}) => (
  <div className="border-b border-gray-200 dark:border-gray-700">
    <div className="px-4 pt-3 pb-1 text-xs font-semibold uppercase tracking-widest text-gray-400 dark:text-gray-500">
      {title}
    </div>
    <StructuredListWrapper isCondensed className="!mb-0">
      <StructuredListBody>{children}</StructuredListBody>
    </StructuredListWrapper>
  </div>
);

export const InfoRow: FC<{ label: string; value: string }> = ({
  label,
  value,
}) => (
  <StructuredListRow>
    <StructuredListCell className="!text-sm text-gray-500 dark:text-gray-400 lowercase tracking-wide">
      {label}
    </StructuredListCell>
    <StructuredListCell className="!text-sm font-medium text-gray-900 dark:text-gray-100 text-right truncate">
      {value}
    </StructuredListCell>
  </StructuredListRow>
);

export const MetricCard: FC<{ label: string; value: string }> = ({
  label,
  value,
}) => (
  <div className="flex flex-col gap-0.5">
    <span className="text-xs uppercase tracking-wide text-gray-400 dark:text-gray-500 truncate">
      {label.replace(/_/g, ' ')}
    </span>
    <span className="text-sm font-semibold text-gray-900 dark:text-gray-100 tabular-nums">
      {value}
    </span>
  </div>
);

// ---------------------------------------------------------------------------
// Metrics computation
// ---------------------------------------------------------------------------

function computeMetrics(events: EventEntry[]): Record<string, string | number> {
  const m: Record<string, string | number> = {
    messages_sent: events.filter(
      e => e.type === 'userMessage' && e.payload?.completed,
    ).length,
    messages_received: events.filter(e => e.type === 'assistantMessage').length,
    pipeline_events: events.filter(e => e.type === 'pipelineEvent').length,
  };

  // Walk in reverse to get the latest value for each key.
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i];

    // Server-emitted ConversationMetric packets (stt_latency_ms, llm_ttft_ms, etc.)
    if (e.type === 'metric') {
      const list: Array<{ name: string; value: string }> =
        e.payload?.metricsList ?? [];
      for (const { name, value } of list) {
        if (name && !(name in m)) m[name] = value;
      }
      continue;
    }

    // Pipeline events — extract well-known fields
    if (e.type !== 'pipelineEvent') continue;
    const { name, dataMap } = e.payload as {
      name: string;
      dataMap: Array<[string, string]>;
    };
    const data = Object.fromEntries(dataMap ?? []);
    const type = data['type'];

    if (
      name === 'llm' &&
      type === 'provider_metrics' &&
      !('llm_input_tokens' in m)
    ) {
      if (data['input_tokens']) m['llm_input_tokens'] = data['input_tokens'];
      if (data['output_tokens']) m['llm_output_tokens'] = data['output_tokens'];
    }
    if (name === 'stt' && type === 'completed' && !('stt_words' in m)) {
      if (data['word_count']) m['stt_words'] = data['word_count'];
    }
    if (name === 'tts' && type === 'completed' && !('tts_audio_kb' in m)) {
      if (data['audio_bytes'])
        m['tts_audio_kb'] =
          `${Math.round(Number(data['audio_bytes']) / 1024)} KB`;
    }
  }

  return m;
}
