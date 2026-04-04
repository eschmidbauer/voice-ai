import { Dropdown } from '@/app/components/dropdown';
import { PrimaryButton, GhostButton, IconOnlyButton } from '@/app/components/carbon/button';
import { PhoneOutgoing, ArrowLeft } from '@carbon/icons-react';
import {
  GreenNoticeBlock,
  RedNoticeBlock,
} from '@/app/components/container/message/notice-block';
import { Tabs, TabList, Tab } from '@carbon/react';
import {
  JsonTextarea,
  NumberTextarea,
  ParagraphTextarea,
  TextTextarea,
  UrlTextarea,
} from '@/app/components/form/textarea';
import { InputVarForm } from '@/app/pages/endpoint/view/try-playground/experiment-prompt/components/input-var-form';
import {
  ConfigBlock,
  InfoRow,
  VoiceAgent,
} from '@/app/pages/preview-agent/voice-agent/voice-agent';
import {
  PHONE_COUNTRIES,
  DEFAULT_COUNTRY,
  Country,
} from '@/app/pages/preview-agent/voice-agent/phone-agent-constants';
import { CONFIG } from '@/configs';
import { useCurrentCredential } from '@/hooks/use-credential';
import { InputVarType } from '@/models/common';
import { randomMeaningfullName } from '@/utils';
import { getStatusMetric } from '@/utils/metadata';
import {
  AgentConfig,
  Channel,
  ConnectionConfig,
  InputOptions,
  StringToAny,
  CreatePhoneCall,
  AssistantDefinition,
  CreatePhoneCallRequest,
  Assistant,
  GetAssistant,
  GetAssistantRequest,
  Variable,
} from '@rapidaai/react';
import React, { ChangeEvent, useEffect, useMemo, useState } from 'react';
import { Navigate, useParams, useSearchParams } from 'react-router-dom';
import { PageLoader } from '@/app/components/loader/page-loader';

/**
 *
 * @returns
 */
export const PublicPreviewVoiceAgent = () => {
  const [searchParams] = useSearchParams();
  const { assistantId } = useParams();
  const authId = searchParams.get('authId');
  const token = searchParams.get('token');

  if (!assistantId || !token) {
    return <Navigate to="/404" replace />;
  }

  return (
    <VoiceAgent
      debug={false}
      connectConfig={ConnectionConfig.DefaultConnectionConfig(
        ConnectionConfig.WithSDK({
          ApiKey: token,
          UserId: '' + (authId || 'public_user'),
        }),
      ).withCustomEndpoint(CONFIG.connection)}
      agentConfig={new AgentConfig(
        assistantId,
        new InputOptions([Channel.Audio, Channel.Text], Channel.Text),
      )
        .addMetadata('authId', StringToAny('' + (authId || 'public_user')))
        .setUserIdentifier(authId || randomMeaningfullName('public'))}
    />
  );
};

//
export const PreviewVoiceAgent = () => {
  const { user, authId, token, projectId } = useCurrentCredential();
  const { assistantId } = useParams();

  if (!assistantId || !user?.name) {
    return <Navigate to="/404" replace />;
  }

  return (
    <VoiceAgent
      debug={true}
      connectConfig={ConnectionConfig.DefaultConnectionConfig(
        ConnectionConfig.WithDebugger({
          authorization: token,
          userId: authId,
          projectId: projectId,
        }),
      ).withCustomEndpoint(CONFIG.connection)}
      agentConfig={new AgentConfig(
        assistantId,
        new InputOptions([Channel.Audio, Channel.Text], Channel.Text),
      )
        .setUserIdentifier(authId, user.name)
        .addKeywords([user.name])
        .addMetadata('authId', StringToAny(authId))
        .addMetadata('projectId', StringToAny(projectId))}
      // .addCustomOption('listen.language', StringToAny('en'))
      // .addCustomOption('speak.language', StringToAny('en'))
      // .addCustomOption('listen.model', StringToAny('nova-3'))}
    />
  );
};

// ---------------------------------------------------------------------------
// Phone Agent
// ---------------------------------------------------------------------------

type PhoneCallStatus = 'idle' | 'calling' | 'success' | 'failed';
type PhoneDebugTab = 'configuration' | 'arguments';

//
export const PreviewPhoneAgent = () => {
  const { authId, token, projectId } = useCurrentCredential();
  const connectionCfg = ConnectionConfig.DefaultConnectionConfig(
    ConnectionConfig.WithPersonalToken({
      Authorization: token,
      AuthId: authId,
      ProjectId: projectId,
    }),
  ).withCustomEndpoint(CONFIG.connection);

  const { assistantId } = useParams();
  const [assistant, setAssistant] = useState<Assistant | null>(null);
  const [variables, setVariables] = useState<Variable[]>([]);
  const [country, setCountry] = useState<Country>(DEFAULT_COUNTRY);
  const [phoneNumber, setPhoneNumber] = useState('');
  const [callStatus, setCallStatus] = useState<PhoneCallStatus>('idle');
  const [errorMessage, setErrorMessage] = useState('');
  const [argumentMap, setArgumentMap] = useState<Map<string, string>>(
    new Map(),
  );
  const [searchQuery, setSearchQuery] = useState('');

  const filteredCountries = useMemo(() => {
    if (!searchQuery) return PHONE_COUNTRIES;
    const q = searchQuery.toLowerCase();
    return PHONE_COUNTRIES.filter(
      c =>
        c.name.toLowerCase().includes(q) ||
        c.value.includes(q) ||
        c.code.toLowerCase().includes(q),
    );
  }, [searchQuery]);

  useEffect(() => {
    if (!assistantId) return;
    const request = new GetAssistantRequest();
    const assistantDef = new AssistantDefinition();
    assistantDef.setAssistantid(assistantId);
    request.setAssistantdefinition(assistantDef);
    GetAssistant(connectionCfg, request)
      .then(response => {
        if (response?.getSuccess()) {
          setAssistant(response.getData()!);
          const pmtVars = response
            .getData()
            ?.getAssistantprovidermodel()
            ?.getTemplate()
            ?.getPromptvariablesList();
          if (pmtVars) {
            setVariables(pmtVars);
            pmtVars.forEach(v => {
              if (v.getDefaultvalue())
                onChangeArgument(v.getName(), v.getDefaultvalue());
            });
          }
        }
      })
      .catch(() => {});
  }, []);

  if (!assistantId) {
    return <Navigate to="/404" replace />;
  }

  const onChangeArgument = (k: string, vl: string) => {
    setArgumentMap(prev => {
      const m = new Map(prev);
      m.set(k, vl);
      return m;
    });
  };

  const validatePhoneNumber = () => {
    if (!country.name) {
      setErrorMessage('Please select a country.');
      return false;
    }
    if (
      (country.name !== 'Other' && phoneNumber.length < 7) ||
      phoneNumber.length > 15
    ) {
      setErrorMessage('Please enter a valid phone number.');
      return false;
    }
    return true;
  };

  const handleSubmit = () => {
    if (!validatePhoneNumber()) return;
    setErrorMessage('');
    setCallStatus('calling');

    const phoneCallRequest = new CreatePhoneCallRequest();
    const assistantDef = new AssistantDefinition();
    assistantDef.setAssistantid(assistantId);
    assistantDef.setVersion('latest');
    phoneCallRequest.setAssistant(assistantDef);
    argumentMap.forEach((value, key) => {
      phoneCallRequest.getArgsMap().set(key, StringToAny(value));
    });
    phoneCallRequest.setTonumber(country.value + phoneNumber);

    CreatePhoneCall(connectionCfg, phoneCallRequest)
      .then(x => {
        if (x.getSuccess()) {
          const status = getStatusMetric(x.getData()?.getMetricsList());
          if (status === 'FAILED') {
            setCallStatus('failed');
            setErrorMessage('Unable to start the call, please try again.');
            return;
          }
          setCallStatus('success');
          return;
        }
        setCallStatus('failed');
        const err = x.getError();
        setErrorMessage(
          err?.getHumanmessage() ||
            'Unable to start the call, please try again.',
        );
      })
      .catch(() => {
        setCallStatus('failed');
        setErrorMessage('Unable to start the call, please try again.');
      });
  };

  const handleReset = () => {
    setPhoneNumber('');
    setCallStatus('idle');
    setErrorMessage('');
  };

  if (!assistant) return <PageLoader />;

  const deployment = assistant.getPhonedeployment() ?? null;
  const stt = deployment?.getInputaudio() ?? null;
  const tts = deployment?.getOutputaudio() ?? null;
  const model = assistant.getAssistantprovidermodel() ?? null;

  return (
    <div className="h-dvh flex p-8 text-sm/6 w-full gap-3 md:gap-6">
      {/* ── Left: phone call form ───────────────────────────────────── */}
      <div className="flex flex-col overflow-hidden h-full w-2/3 border border-gray-200 dark:border-gray-700 rounded bg-white dark:bg-gray-950">
        {/* Header */}
        <div className="flex items-center gap-1.5 px-3 py-2 border-b border-gray-200 dark:border-gray-800 shrink-0">
          <IconOnlyButton
            kind="ghost"
            size="sm"
            renderIcon={ArrowLeft}
            iconDescription="Back to Assistant"
            onClick={() => {
              window.location.href = `/deployment/assistant/${assistantId}/overview`;
            }}
          />
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
            Back to Assistant
          </span>
        </div>

        {/* Body */}
        <div className="flex-1 flex flex-col items-center justify-center px-8">
          <div className="w-full max-w-lg space-y-6">
            <div className="space-y-1">
              <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
                Make a Phone Call
              </h2>
              <p className="text-gray-500 dark:text-gray-400">
                Enter a phone number to start a call with your assistant.
              </p>
            </div>

            <div
              className="flex items-stretch h-10 border border-gray-300 dark:border-gray-700 focus-within:border-blue-600 dark:focus-within:border-blue-600 transition-colors"
            >
              <div className="w-52 shrink-0 border-r border-gray-300 dark:border-gray-700">
                <Dropdown
                  className="h-full bg-white dark:bg-gray-950 border-none! outline-hidden! focus-within:border-none! focus-within:outline-hidden! [&_input]:h-full [&>div]:h-full"
                  currentValue={country}
                  setValue={v => setCountry(v)}
                  allValue={filteredCountries}
                  placeholder="Select country"
                  searchable
                  onSearching={(e: ChangeEvent<HTMLInputElement>) =>
                    setSearchQuery(e.target.value)
                  }
                  option={c => (
                    <span className="inline-flex items-center gap-2 max-w-full text-sm font-medium">
                      <span className="truncate capitalize">
                        {c.name} ({c.value})
                      </span>
                    </span>
                  )}
                  label={c => (
                    <span className="inline-flex items-center gap-2 max-w-full text-sm font-medium">
                      <span className="truncate capitalize">
                        {c.name} ({c.value})
                      </span>
                    </span>
                  )}
                />
              </div>
              <input
                type="tel"
                placeholder="Enter your phone number"
                className="flex-1 h-full px-4 text-sm bg-transparent outline-none text-gray-900 dark:text-gray-100 placeholder:text-gray-400 dark:placeholder:text-gray-500"
                value={phoneNumber}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => {
                  setPhoneNumber(e.target.value);
                  setErrorMessage('');
                }}
              />
            </div>

            {errorMessage && (
              <RedNoticeBlock>{errorMessage}</RedNoticeBlock>
            )}

            {callStatus === 'success' && (
              <GreenNoticeBlock>
                Call has been created successfully.
              </GreenNoticeBlock>
            )}

            <div className="flex items-center justify-between">
              {callStatus === 'success' ? (
                <GhostButton size="sm" onClick={handleReset}>
                  Make another call
                </GhostButton>
              ) : (
                <span />
              )}
              <PrimaryButton
                size="md"
                renderIcon={PhoneOutgoing}
                onClick={handleSubmit}
                isLoading={callStatus === 'calling'}
              >
                Start Call
              </PrimaryButton>
            </div>
          </div>
        </div>
      </div>

      {/* ── Right: debugger panel ───────────────────────────────────── */}
      <div className="shrink-0 flex flex-col overflow-hidden border border-gray-200 dark:border-gray-700 w-1/3 rounded bg-white dark:bg-gray-950">
        <PhoneAgentDebugger
          assistant={assistant}
          deployment={deployment ? deployment : undefined}
          stt={stt}
          tts={tts}
          model={model}
          variables={variables}
          onChangeArgument={onChangeArgument}
        />
      </div>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Phone Agent Debugger (right panel)
// ---------------------------------------------------------------------------

const PhoneAgentDebugger: React.FC<{
  assistant: Assistant;
  deployment: ReturnType<Assistant['getPhonedeployment']>;
  stt: any;
  tts: any;
  model: any;
  variables: Variable[];
  onChangeArgument: (k: string, v: string) => void;
}> = ({
  assistant,
  deployment,
  stt,
  tts,
  model,
  variables,
  onChangeArgument,
}) => {
  const [tab, setTab] = useState<PhoneDebugTab>('configuration');

  return (
    <div className="flex flex-col h-full overflow-hidden text-sm">
      {/* Tab bar */}
      <Tabs
        selectedIndex={tab === 'configuration' ? 0 : 1}
        onChange={({ selectedIndex }) =>
          setTab(selectedIndex === 0 ? 'configuration' : 'arguments')
        }
      >
        <TabList contained aria-label="Phone debugger tabs">
          <Tab>Configuration</Tab>
          <Tab>Arguments</Tab>
        </TabList>
      </Tabs>

      {/* ── configuration tab ── */}
      {tab === 'configuration' && (
        <div className="flex-1 min-h-0 overflow-y-auto">
          <ConfigBlock title="assistant">
            <InfoRow label="name" value={assistant.getName()} />
            {assistant.getDescription() && (
              <InfoRow label="description" value={assistant.getDescription()} />
            )}
          </ConfigBlock>

          <ConfigBlock title="telephony">
            {deployment?.getPhoneprovidername() && (
              <InfoRow
                label="provider"
                value={deployment.getPhoneprovidername()}
              />
            )}
            <InfoRow
              label="input mode"
              value={'Text' + (deployment?.getInputaudio() ? ', Audio' : '')}
            />
            <InfoRow
              label="output mode"
              value={'Text' + (deployment?.getOutputaudio() ? ', Audio' : '')}
            />
          </ConfigBlock>

          {stt && (
            <ConfigBlock title="stt">
              <InfoRow label="provider" value={stt.getAudioprovider()} />
              {stt
                .getAudiooptionsList()
                .filter((d: any) => d.getValue())
                .filter((d: any) => d.getKey().startsWith('listen.'))
                .map((d: any) => (
                  <InfoRow
                    key={d.getKey()}
                    label={d.getKey()}
                    value={d.getValue()}
                  />
                ))}
            </ConfigBlock>
          )}

          {tts && (
            <ConfigBlock title="tts">
              <InfoRow label="provider" value={tts.getAudioprovider()} />
              {tts
                .getAudiooptionsList()
                .filter((d: any) => d.getValue())
                .filter((d: any) => d.getKey().startsWith('speak.'))
                .map((d: any) => (
                  <InfoRow
                    key={d.getKey()}
                    label={d.getKey()}
                    value={d.getValue()}
                  />
                ))}
            </ConfigBlock>
          )}

          {model && (
            <ConfigBlock title="llm">
              <InfoRow label="provider" value={model.getModelprovidername()} />
              {model.getAssistantmodeloptionsList().map((m: any) => (
                <InfoRow
                  key={m.getKey()}
                  label={m.getKey()}
                  value={m.getValue()}
                />
              ))}
            </ConfigBlock>
          )}
        </div>
      )}

      {/* ── arguments tab ── */}
      {tab === 'arguments' && (
        <div className="flex-1 min-h-0 overflow-y-auto">
          {variables.length > 0 ? (
            <div className="[&_label]:!text-sm [&_label]:!leading-6 [&_label]:!py-2 [&_label]:!px-3 [&_textarea]:!text-sm [&_textarea]:!leading-6 [&_textarea]:!px-3 [&_textarea]:!py-2">
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
    </div>
  );
};
