import {
  Assistant,
  AssistantConversation,
  AssistantConversationMessage,
  ConnectionConfig,
  GetAllAssistantConversation,
} from '@rapidaai/react';
import { connectionConfig } from '@/configs';
import { toDate, toDateString } from '@/utils/date';
import {
  getStatusMetric,
  getTotalTokenMetric,
  findMetricByName,
  isConversationCompleted,
} from '@/utils/metadata';
import {
  XAxis,
  Tooltip,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Bar,
  BarChart,
  YAxis,
  AreaChart,
  Area,
} from 'recharts';
import {
  NameType,
  ValueType,
} from 'recharts/types/component/DefaultTooltipContent';
import { ContentType } from 'recharts/types/component/Tooltip';
import { useAssistantTracePageStore } from '@/hooks/use-assistant-trace-page-store';
import { FC, useEffect, useState } from 'react';
import { cn } from '@/utils';
import { useCurrentCredential } from '@/hooks/use-credential';
import { Dropdown, Tile } from '@carbon/react';

const CHART_COLORS = [
  'var(--cds-interactive, #1e40af)',
  '#22d3ee',
  '#f59e0b',
  '#10b981',
  '#f43f5e',
  '#8b5cf6',
];

const DATE_RANGES = [
  { id: 'last_24_hours', text: 'Last 24 hours' },
  { id: 'last_3_days', text: 'Last 3 days' },
  { id: 'last_7_days', text: 'Last 7 days' },
  { id: 'last_30_days', text: 'Last 30 days' },
];

const AUTO_REFRESH_OPTIONS = [
  { id: '0', text: 'Off' },
  { id: '5', text: 'Every 5 min' },
  { id: '10', text: 'Every 10 min' },
  { id: '30', text: 'Every 30 min' },
];

const getStartDate = (range: string): Date => {
  const now = new Date();
  switch (range) {
    case 'last_24_hours': return new Date(now.getTime() - 24 * 60 * 60 * 1000);
    case 'last_3_days': return new Date(now.getTime() - 3 * 24 * 60 * 60 * 1000);
    case 'last_7_days': return new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
    default: return new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
  }
};

export const AssistantAnalytics: FC<{ assistant: Assistant }> = props => {
  const assistantTraceAction = useAssistantTracePageStore();
  const [autoRefreshInterval, setAutoRefreshInterval] = useState<null | number>(null);
  const [selectedRange, setSelectedRange] = useState<string>('last_30_days');
  const [convList, setConvList] = useState<AssistantConversation[]>([]);
  const [loading, setLoading] = useState(true);
  const { authId, token, projectId } = useCurrentCredential();

  const getDateRangeCriteria = (range: string) => ({
    k: 'assistant_conversation_messages.created_date',
    v: toDateString(getStartDate(range)),
    logic: '>=',
  });

  const getConversationDateCriteria = (range: string) => ([{
    key: 'assistant_conversations.created_date',
    value: toDateString(getStartDate(range)),
    logic: '>=',
  }]);

  useEffect(() => {
    assistantTraceAction.clear();
    assistantTraceAction.addCriterias([getDateRangeCriteria(selectedRange)]);
  }, []);

  useEffect(() => {
    setLoading(true);
    fetchAssistantMessages();
    fetchConversations();
  }, [props.assistant.getId(), projectId, selectedRange, JSON.stringify(assistantTraceAction.criteria), token, authId]);

  const fetchAssistantMessages = () => {
    assistantTraceAction.setPageSize(0);
    assistantTraceAction.setFields(['metadata', 'metric']);
    assistantTraceAction.addCriterias([getDateRangeCriteria(selectedRange)]);
    assistantTraceAction.getAssistantMessages(props.assistant.getId(), projectId, token, authId, () => { setLoading(false); }, () => { setLoading(false); });
  };

  const fetchConversations = () => {
    GetAllAssistantConversation(
      connectionConfig,
      props.assistant.getId(),
      1,
      0,
      getConversationDateCriteria(selectedRange),
      (err, res) => {
        if (res?.getSuccess()) setConvList(res.getDataList());
      },
      { authorization: token, 'x-auth-id': authId, 'x-project-id': projectId },
    );
  };

  useEffect(() => {
    let id: NodeJS.Timeout | null = null;
    if (autoRefreshInterval && autoRefreshInterval > 0) id = setInterval(() => { fetchAssistantMessages(); fetchConversations(); }, autoRefreshInterval * 60 * 1000);
    return () => { if (id) clearInterval(id); };
  }, [autoRefreshInterval]);

  // ── Derive conversation groups ──
  const conversationsMap = assistantTraceAction.assistantMessages.reduce((acc, message) => {
    const id = message.getAssistantconversationid();
    if (!acc.has(id)) acc.set(id, []);
    acc.get(id)!.push(message);
    return acc;
  }, new Map<string, AssistantConversationMessage[]>());

  const conversations = Array.from(conversationsMap.values());
  const totalMessages = assistantTraceAction.assistantMessages.length;

  // ── All counts from conversations API for consistency ──
  const totalSessions = convList.length;
  const completedConversations = convList.filter(c =>
    isConversationCompleted(c.getMetricsList?.() || []),
  ).length;
  const failedConversations = convList.filter(c => {
    const s = findMetricByName(c.getMetricsList?.() || [], 'status').toUpperCase();
    return s === 'FAILED' || s === 'ERROR';
  }).length;
  const activeConversations = totalSessions - completedConversations - failedConversations;

  // ── Duration: from message-grouped conversations (message-level data) ──
  const durations = conversations.map(conv => {
    const sorted = [...conv].sort((a, b) => toDate(a.getCreateddate()!).getTime() - toDate(b.getCreateddate()!).getTime());
    return (toDate(sorted[sorted.length - 1].getCreateddate()!).getTime() - toDate(sorted[0].getCreateddate()!).getTime()) / 1000;
  });
  const totalDuration = durations.reduce((sum, d) => sum + d, 0);
  const avgDuration = totalSessions > 0 ? totalDuration / totalSessions : 0;

  // ── Message-level metrics: STT, TTS & LLM latency ──
  const sttLatencies: number[] = [];
  const ttsLatencies: number[] = [];
  const llmLatencies: number[] = [];
  assistantTraceAction.assistantMessages.forEach(m => {
    const metrics = m.getMetricsList();
    const stt = findMetricByName(metrics, 'stt_latency_ms');
    if (stt) sttLatencies.push(Number(stt));
    const tts = findMetricByName(metrics, 'tts_latency_ms');
    if (tts) ttsLatencies.push(Number(tts));
    const llm = findMetricByName(metrics, 'llm_latency_ms');
    if (llm) llmLatencies.push(Number(llm));
  });
  const avgSttLatency = sttLatencies.length > 0 ? sttLatencies.reduce((a, b) => a + b, 0) / sttLatencies.length : 0;
  const avgTtsLatency = ttsLatencies.length > 0 ? ttsLatencies.reduce((a, b) => a + b, 0) / ttsLatencies.length : 0;
  const avgLlmLatency = llmLatencies.length > 0 ? llmLatencies.reduce((a, b) => a + b, 0) / llmLatencies.length : 0;
  const totalSttDurationSec = sttLatencies.reduce((a, b) => a + b, 0) / 1000;
  const totalTtsDurationSec = ttsLatencies.reduce((a, b) => a + b, 0) / 1000;

  // ── Token metrics ──
  const totalTokens = assistantTraceAction.assistantMessages.reduce((sum, m) => sum + getTotalTokenMetric(m.getMetricsList()), 0);

  // ── Language from user messages only ──
  const languageCounts: Record<string, number> = {};
  let userMessageCount = 0;
  assistantTraceAction.assistantMessages.forEach(item => {
    const msgId = item.getMessageid?.() || '';
    const role = item.getRole?.()?.toLowerCase() || '';
    const isUser = msgId.startsWith('user-') || role === 'user';
    if (!isUser) return;
    userMessageCount++;
    const lang = item.getMetadataList().find(m => m.getKey() === 'language')?.getValue();
    if (lang) languageCounts[lang] = (languageCounts[lang] || 0) + 1;
  });
  const languageData = Object.entries(languageCounts).map(([language, count]) => ({
    language,
    count,
    percentage: ((count / Math.max(userMessageCount, 1)) * 100).toFixed(1),
  }));

  // ── Source distribution ──
  const sourceData = Object.entries(
    assistantTraceAction.assistantMessages.reduce((acc, item) => {
      const source = item.getSource();
      acc[source] = (acc[source] || 0) + 1;
      return acc;
    }, {} as Record<string, number>),
  ).map(([source, count]) => ({ source, count, percentage: ((count / Math.max(totalMessages, 1)) * 100).toFixed(1) }));

  // ── Time-series buckets ──
  const activeSessionsData = (() => {
    const now = new Date();
    let interval: number;
    let formatLabel: (d: Date) => string;
    switch (selectedRange) {
      case 'last_24_hours': interval = 30; formatLabel = d => `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`; break;
      case 'last_3_days': interval = 120; formatLabel = d => `${toDateString(d)} ${d.getHours().toString().padStart(2, '0')}:00`; break;
      case 'last_7_days': interval = 240; formatLabel = d => `${toDateString(d)} ${d.getHours().toString().padStart(2, '0')}:00`; break;
      default: interval = 1440; formatLabel = d => toDateString(d);
    }
    const startTime = new Date();
    startTime.setMinutes(0, 0, 0);
    switch (selectedRange) {
      case 'last_24_hours': startTime.setTime(startTime.getTime() - 24 * 60 * 60 * 1000); break;
      case 'last_3_days': startTime.setTime(startTime.getTime() - 3 * 24 * 60 * 60 * 1000); break;
      case 'last_7_days': startTime.setTime(startTime.getTime() - 7 * 24 * 60 * 60 * 1000); break;
      default: startTime.setTime(startTime.getTime() - 30 * 24 * 60 * 60 * 1000);
    }
    const buckets: Array<{ date: Date; total: number; sttMs: number; ttsMs: number; llmMs: number; sttCount: number; ttsCount: number; llmCount: number }> = [];
    for (let t = startTime.getTime(); t < now.getTime(); t += interval * 60 * 1000)
      buckets.push({ date: new Date(t), total: 0, sttMs: 0, ttsMs: 0, llmMs: 0, sttCount: 0, ttsCount: 0, llmCount: 0 });

    assistantTraceAction.assistantMessages.forEach(m => {
      const idx = Math.floor((toDate(m.getCreateddate()!).getTime() - startTime.getTime()) / (interval * 60 * 1000));
      if (idx >= 0 && idx < buckets.length) {
        buckets[idx].total += 1;
        const stt = findMetricByName(m.getMetricsList(), 'stt_latency_ms');
        if (stt) { buckets[idx].sttMs += Number(stt); buckets[idx].sttCount += 1; }
        const tts = findMetricByName(m.getMetricsList(), 'tts_latency_ms');
        if (tts) { buckets[idx].ttsMs += Number(tts); buckets[idx].ttsCount += 1; }
        const llm = findMetricByName(m.getMetricsList(), 'llm_latency_ms');
        if (llm) { buckets[idx].llmMs += Number(llm); buckets[idx].llmCount += 1; }
      }
    });
    return buckets.map(b => ({
      dateHour: formatLabel(b.date),
      total: b.total,
      sttLatency: b.sttCount > 0 ? Math.round(b.sttMs / b.sttCount) : 0,
      ttsLatency: b.ttsCount > 0 ? Math.round(b.ttsMs / b.ttsCount) : 0,
      llmLatency: b.llmCount > 0 ? Math.round(b.llmMs / b.llmCount) : 0,
      label: `From: ${b.date.toISOString().split('.')[0].replace('T', ' ')}`,
    }));
  })();

  const hasData = totalMessages > 0 || totalSessions > 0;

  return (
    <div className="w-full p-4 space-y-4">
      {/* ── Toolbar ── */}
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-widest text-gray-500 dark:text-gray-400">
          Summary Dashboard
        </h2>
        <div className="flex items-center gap-2">
          <Dropdown
            id="date-range"
            titleText=""
            hideLabel
            label="Date range"
            size="sm"
            items={DATE_RANGES}
            selectedItem={DATE_RANGES.find(r => r.id === selectedRange)}
            itemToString={(item: any) => item?.text || ''}
            onChange={({ selectedItem }) => { if (selectedItem) setSelectedRange(selectedItem.id); }}
            className="min-w-[160px]"
          />
          <Dropdown
            id="auto-refresh"
            titleText=""
            hideLabel
            label="Auto-refresh"
            size="sm"
            items={AUTO_REFRESH_OPTIONS}
            selectedItem={AUTO_REFRESH_OPTIONS.find(o => o.id === String(autoRefreshInterval || 0))}
            itemToString={(item: any) => item?.text || ''}
            onChange={({ selectedItem }) => { if (selectedItem) setAutoRefreshInterval(selectedItem.id === '0' ? null : Number(selectedItem.id)); }}
            className="min-w-[140px]"
          />
        </div>
      </div>

      {loading && !hasData ? (
        <div className="flex items-center justify-center h-64 text-sm text-gray-500 dark:text-gray-400">
          Loading analytics…
        </div>
      ) : !hasData ? (
        <div className="flex items-center justify-center h-64 text-sm text-gray-500 dark:text-gray-400">
          No data available for the selected period.
        </div>
      ) : (
        <>
          {/* ── Top section: metric cards (left) + summary block (right) ── */}
          <div className="grid grid-cols-1 xl:grid-cols-[1fr_280px] xl:items-start gap-4">
            {/* Left — two grouped tiles */}
            <div className="space-y-4">
              {/* Row 1: Session counts */}
              <Tile className="!rounded-none !p-0 !bg-white dark:!bg-transparent border border-gray-200 dark:border-gray-800">
                <div className="px-4 pt-4 pb-1">
                  <h3 className="text-sm font-semibold">Sessions</h3>
                </div>
                <div className="grid grid-cols-2 md:grid-cols-4">
                  <MetricCell label="Sessions" value={totalSessions} />
                  <MetricCell label="Active" value={activeConversations} />
                  <MetricCell label="Completed" value={completedConversations} />
                  <MetricCell label="Failed" value={failedConversations} />
                </div>
              </Tile>
              {/* Row 2: Averages */}
              <Tile className="!rounded-none !p-0 !bg-white dark:!bg-transparent border border-gray-200 dark:border-gray-800">
                <div className="px-4 pt-4 pb-1">
                  <h3 className="text-sm font-semibold">Average Latency</h3>
                </div>
                <div className="grid grid-cols-2 md:grid-cols-4">
                  <MetricCell label="Duration" value={Math.round(avgDuration)} unit="s" />
                  <MetricCell label="STT" value={Math.round(avgSttLatency)} unit="ms" />
                  <MetricCell label="TTS" value={Math.round(avgTtsLatency)} unit="ms" />
                  <MetricCell label="LLM" value={Math.round(avgLlmLatency)} unit="ms" />
                </div>
              </Tile>
            </div>

            {/* Right — totals block */}
            <Tile className="!rounded-none !p-0 !bg-white dark:!bg-transparent border border-gray-200 dark:border-gray-800 flex flex-col justify-between">
              <CompactMetricCell label="Tokens" value={totalTokens} />
              <CompactMetricCell label="STT Duration" value={Math.round(totalSttDurationSec)} unit="s" />
              <CompactMetricCell label="TTS Duration" value={Math.round(totalTtsDurationSec)} unit="s" />
              <CompactMetricCell label="Duration" value={Math.round(totalDuration)} unit="s" />
            </Tile>
          </div>

          {/* ── Charts row — language + latency + source ── */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {/* Language gauge — user messages only */}
            <ChartTile title="Languages" subtitle="User messages">
              <div className="flex flex-col items-center py-4">
                <div className="relative h-[140px] w-full">
                  <ResponsiveContainer width="100%" height="100%">
                    <PieChart>
                      <Pie
                        data={languageData.length > 0 ? languageData : [{ language: 'No data', count: 1 }]}
                        cx="50%"
                        cy="80%"
                        startAngle={180}
                        endAngle={0}
                        innerRadius={60}
                        outerRadius={90}
                        dataKey="count"
                        nameKey="language"
                        stroke="none"
                      >
                        {languageData.length > 0
                          ? languageData.map((_, i) => <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />)
                          : <Cell fill="#e0e0e0" />
                        }
                      </Pie>
                    </PieChart>
                  </ResponsiveContainer>
                  <div className="absolute bottom-2 left-1/2 -translate-x-1/2 text-center">
                    <p className="text-lg font-bold tabular-nums">{languageData.length}</p>
                    <p className="text-[10px] text-gray-400 uppercase">Languages</p>
                  </div>
                </div>
                <div className="flex flex-wrap justify-center gap-3 px-4 pb-2">
                  {languageData.map((item, i) => (
                    <div key={item.language} className="flex items-center gap-1.5 text-xs">
                      <div className="w-2 h-2 shrink-0" style={{ backgroundColor: CHART_COLORS[i % CHART_COLORS.length] }} />
                      <span className="text-gray-600 dark:text-gray-400 capitalize">{item.language}</span>
                      <span className="font-semibold tabular-nums">{item.percentage}%</span>
                    </div>
                  ))}
                </div>
              </div>
            </ChartTile>

            {/* Latency sparkline — STT, TTS & LLM */}
            <ChartTile title="Latency">
              <div className="flex items-center gap-6 px-4 pt-2 pb-1">
                <div>
                  <p className="text-[10px] text-gray-400 uppercase">STT</p>
                  <p className="text-xl font-light tabular-nums">{Math.round(avgSttLatency)} <span className="text-xs text-gray-500">ms</span></p>
                </div>
                <div>
                  <p className="text-[10px] text-gray-400 uppercase">TTS</p>
                  <p className="text-xl font-light tabular-nums">{Math.round(avgTtsLatency)} <span className="text-xs text-gray-500">ms</span></p>
                </div>
                <div>
                  <p className="text-[10px] text-gray-400 uppercase">LLM</p>
                  <p className="text-xl font-light tabular-nums">{Math.round(avgLlmLatency)} <span className="text-xs text-gray-500">ms</span></p>
                </div>
              </div>
              <div className="h-[120px] px-2">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={activeSessionsData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
                    <defs>
                      <linearGradient id="sttGradient" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor="#f59e0b" stopOpacity={0.3} />
                        <stop offset="100%" stopColor="#f59e0b" stopOpacity={0.02} />
                      </linearGradient>
                      <linearGradient id="ttsGradient" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor="var(--cds-interactive, #1e40af)" stopOpacity={0.3} />
                        <stop offset="100%" stopColor="var(--cds-interactive, #1e40af)" stopOpacity={0.02} />
                      </linearGradient>
                      <linearGradient id="llmGradient" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor="#10b981" stopOpacity={0.3} />
                        <stop offset="100%" stopColor="#10b981" stopOpacity={0.02} />
                      </linearGradient>
                    </defs>
                    <Area type="monotone" dataKey="sttLatency" stroke="#f59e0b" strokeWidth={1.5} fill="url(#sttGradient)" dot={false} activeDot={{ r: 3 }} />
                    <Area type="monotone" dataKey="ttsLatency" stroke="var(--cds-interactive, #1e40af)" strokeWidth={1.5} fill="url(#ttsGradient)" dot={false} activeDot={{ r: 3 }} />
                    <Area type="monotone" dataKey="llmLatency" stroke="#10b981" strokeWidth={1.5} fill="url(#llmGradient)" dot={false} activeDot={{ r: 3 }} />
                    <Tooltip
                      content={(({ active, payload }) => {
                        if (!active || !payload?.length) return null;
                        const labelMap: Record<string, string> = { sttLatency: 'STT', ttsLatency: 'TTS', llmLatency: 'LLM' };
                        return (
                          <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 shadow-lg px-3 py-2 text-sm min-w-[140px]">
                            <p className="text-gray-400 text-xs mb-1.5">{payload[0]?.payload?.label}</p>
                            {payload.map((p: any) => (
                              <div key={p.dataKey} className="flex items-center gap-2">
                                <div className="w-2 h-2" style={{ backgroundColor: p.stroke }} />
                                <span className="text-gray-600 dark:text-gray-300 uppercase text-xs">{labelMap[p.dataKey] || p.dataKey}</span>
                                <span className="ml-auto font-semibold tabular-nums">{p.value} ms</span>
                              </div>
                            ))}
                          </div>
                        );
                      }) as ContentType<ValueType, NameType>}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
              <div className="flex justify-center gap-4 px-4 pb-3 text-xs">
                <div className="flex items-center gap-1.5"><div className="w-3 h-0.5 bg-amber-500" /> STT</div>
                <div className="flex items-center gap-1.5"><div className="w-3 h-0.5 bg-blue-700" /> TTS</div>
                <div className="flex items-center gap-1.5"><div className="w-3 h-0.5 bg-emerald-500" /> LLM</div>
              </div>
            </ChartTile>

            {/* Source donut */}
            <ChartTile title="Sources">
              <DonutContent data={sourceData} dataKey="count" nameKey="source" total={totalMessages} />
            </ChartTile>
          </div>

          {/* ── Full-width sessions chart ── */}
          <ChartTile title="Sessions">
            <div className="h-[260px] px-2 py-4">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={activeSessionsData} margin={{ top: 0, right: 16, left: 0, bottom: 0 }}>
                  <YAxis dataKey="total" tickLine={false} axisLine={false} tick={{ fontSize: 11, fill: '#9ca3af' }} width={36} />
                  <XAxis dataKey="dateHour" tickLine={false} axisLine={false} tick={{ fontSize: 11, fill: '#9ca3af' }} interval="preserveStartEnd" />
                  <Tooltip
                    cursor={{ fill: 'var(--cds-interactive, #1e40af)', fillOpacity: 0.06 }}
                    content={(({ active, payload }) => {
                      if (!active || !payload?.length) return null;
                      return (
                        <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 shadow-lg px-3 py-2.5 text-sm min-w-[140px]">
                          <p className="text-gray-400 text-xs mb-1.5">{payload[0]?.payload?.label}</p>
                          <div className="flex items-center gap-2">
                            <div className="w-2 h-2" style={{ backgroundColor: 'var(--cds-interactive, #1e40af)' }} />
                            <span className="text-gray-600 dark:text-gray-300">Messages</span>
                            <span className="ml-auto font-semibold tabular-nums">{payload[0]?.value}</span>
                          </div>
                        </div>
                      );
                    }) as ContentType<ValueType, NameType>}
                  />
                  <Bar dataKey="total" fill="var(--cds-interactive, #1e40af)" fillOpacity={0.85} radius={[2, 2, 0, 0]} maxBarSize={32} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </ChartTile>
        </>
      )}
    </div>
  );
};

// ─── Metric cell (inside a grouped tile) ────────────────────────────────────

const MetricCell: FC<{ label: string; value: number; unit?: string }> = ({
  label, value, unit,
}) => (
  <div className="px-4 py-4">
    <p className="text-xs text-gray-500 dark:text-gray-400 mb-1">{label}</p>
    <div className="flex items-baseline gap-1">
      <span className="text-5xl font-light tabular-nums tracking-tight">
        {isNaN(value) ? '–' : value.toLocaleString()}
      </span>
      {unit && (
        <span className="text-sm font-normal text-gray-400 uppercase">{unit}</span>
      )}
    </div>
  </div>
);

// ─── Compact metric cell (for sidebar) ──────────────────────────────────────

const CompactMetricCell: FC<{ label: string; value: number; unit?: string }> = ({
  label, value, unit,
}) => (
  <div className="px-4 py-2.5">
    <p className="text-xs text-gray-500 dark:text-gray-400 mb-0.5">{label}</p>
    <div className="flex items-baseline gap-1">
      <span className="text-3xl font-light tabular-nums tracking-tight">
        {isNaN(value) ? '–' : value.toLocaleString()}
      </span>
      {unit && (
        <span className="text-xs font-normal text-gray-400 uppercase">{unit}</span>
      )}
    </div>
  </div>
);

// ─── Chart tile wrapper ─────────────────────────────────────────────────────

const ChartTile: FC<{ title: string; subtitle?: string; className?: string; children: React.ReactNode }> = ({
  title, subtitle, className, children,
}) => (
  <Tile className={cn('!rounded-none !p-0 !bg-white dark:!bg-transparent border border-gray-200 dark:border-gray-800', className)}>
    <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700">
      <h3 className="text-sm font-semibold">{title}</h3>
      {subtitle && (
        <p className="text-[10px] text-gray-500 dark:text-gray-400 uppercase">{subtitle}</p>
      )}
    </div>
    {children}
  </Tile>
);

// ─── Donut chart content ────────────────────────────────────────────────────

const DonutContent: FC<{ data: any[]; dataKey: string; nameKey: string; total: number }> = ({
  data, dataKey, nameKey, total,
}) => (
  <>
    <div className="relative h-[200px]">
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie data={data} cx="50%" cy="50%" labelLine={false} outerRadius={80} innerRadius={50} dataKey={dataKey} nameKey={nameKey} stroke="none">
            {data.map((_, i) => <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />)}
          </Pie>
          <Tooltip
            content={(({ active, payload }) => {
              if (!active || !payload?.length) return null;
              const item = payload[0];
              return (
                <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 shadow-lg px-3 py-2 text-sm">
                  <div className="flex items-center gap-2">
                    <div className="w-2.5 h-2.5 shrink-0" style={{ backgroundColor: item.color || '#6366f1' }} />
                    <span className="capitalize">{item.name || 'Unknown'}</span>
                    <span className="ml-3 font-semibold">{item.value}</span>
                  </div>
                </div>
              );
            }) as ContentType<ValueType, NameType>}
          />
        </PieChart>
      </ResponsiveContainer>
      <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
        <div className="text-center">
          <p className="text-lg font-bold tabular-nums">{total}</p>
          <p className="text-[10px] text-gray-400 uppercase">Total</p>
        </div>
      </div>
    </div>
    <div className="px-4 pb-4 pt-2 space-y-2">
      {data.map((item, i) => (
        <div key={item[nameKey]} className="flex items-center gap-2 text-xs">
          <div className="w-2.5 h-2.5 shrink-0" style={{ backgroundColor: CHART_COLORS[i % CHART_COLORS.length] }} />
          <span className="text-gray-600 dark:text-gray-400 truncate flex-1 capitalize">{item[nameKey] || 'Unknown'}</span>
          <span className="font-semibold tabular-nums">{item.percentage}%</span>
          <span className="text-gray-400 tabular-nums">({item.count})</span>
        </div>
      ))}
    </div>
  </>
);
