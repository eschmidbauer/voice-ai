import {
  AssistantPhoneDeployment,
  DeploymentAudioProvider,
} from '@rapidaai/react';
import { Tab } from '@/app/components/tab';
import { cn } from '@/utils';
import { ModalProps } from '@/app/components/base/modal';
import { RightSideModal } from '@/app/components/base/modal/right-side-modal';
import { CONFIG } from '@/configs';
import { CopyButton } from '@/app/components/carbon/button/copy-button';
import { InputHelper } from '@/app/components/input-helper';
import { YellowNoticeBlock } from '@/app/components/container/message/notice-block';
import { ProviderPill } from '@/app/components/pill/provider-model-pill';
import { FC, ReactNode, useMemo } from 'react';
import { DeploymentRow, DeploymentSectionHeader } from '@/app/components/base/modal/deployment-modal-primitives';

interface AssistantPhoneCallDeploymentDialogProps extends ModalProps {
  deployment: AssistantPhoneDeployment;
}

export function AssistantPhoneCallDeploymentDialog(
  props: AssistantPhoneCallDeploymentDialogProps,
) {
  const providerName = props.deployment?.getPhoneprovidername()?.toLowerCase();
  const assistantId = props.deployment?.getAssistantid();
  const mediaHost = CONFIG.connection.media;
  const sipHost = CONFIG.connection.sip;
  const socketHost = CONFIG.connection.socket;

  const webhookUrl = `${mediaHost}/v1/talk/${providerName}/call/${assistantId}?x-api-key={{PROJECT_CRDENTIAL_KEY}}`;
  const eventUrl = `${mediaHost}/v1/talk/${providerName}/event/${assistantId}?x-api-key={{PROJECT_CRDENTIAL_KEY}}`;

  return (
    <RightSideModal
      modalOpen={props.modalOpen}
      setModalOpen={props.setModalOpen}
      className="w-2/3 xl:w-1/3 flex-1"
    >
      <div className="h-12 px-4 flex items-center gap-2 border-b border-gray-200 dark:border-gray-800 shrink-0">
        <span className="text-xs font-medium uppercase tracking-[0.08em] text-gray-500 dark:text-gray-400">
          Deployment
        </span>
        <span className="text-gray-300 dark:text-gray-600">/</span>
        <span className="text-xs font-medium uppercase tracking-[0.08em] text-gray-500 dark:text-gray-400">
          Phone
        </span>
        <span className="text-gray-300 dark:text-gray-600">/</span>
        <span className="text-sm font-semibold text-gray-900 dark:text-gray-100 font-mono truncate">
          {props.deployment.getId()}
        </span>
      </div>
      <div className="flex flex-col flex-1 overflow-auto h-[calc(100vh-48px)]">
        <Tab
          active="Integration"
          className={cn('bg-white dark:bg-gray-900 sticky top-0 z-1')}
          tabs={[
            {
              label: 'Integration',
              element: (
                <div className="divide-y divide-gray-200 dark:divide-gray-800 w-full">
                  {providerName === 'sip' && (
                    <SipIntegrationInstructions
                      sipHost={sipHost}
                      assistantId={assistantId}
                    />
                  )}
                  {providerName === 'asterisk' && (
                    <AsteriskIntegrationInstructions
                      mediaHost={mediaHost}
                      audioSocketHost={socketHost}
                      assistantId={assistantId}
                    />
                  )}
                  {providerName !== 'sip' && providerName !== 'asterisk' && (
                    <>
                      <CodeRow label="Inbound webhook url" value={webhookUrl}>
                        <InputHelper>
                          You can add additional agent arguments as query
                          parameters — e.g.{' '}
                          <code className="text-red-600">
                            `?name=your-name`
                          </code>
                        </InputHelper>
                      </CodeRow>
                      <CodeRow
                        label="Call status / Event callback webhook"
                        value={eventUrl}
                      />
                    </>
                  )}
                </div>
              ),
            },
            {
              label: 'Audio',
              element: (
                <div className="divide-y divide-gray-200 dark:divide-gray-800 w-full">
                  <VoiceInput deployment={props.deployment?.getInputaudio()} />
                  <VoiceOutput
                    deployment={props.deployment?.getOutputaudio()}
                  />
                </div>
              ),
            },
          ]}
        />
      </div>
    </RightSideModal>
  );
}

/* -------------------------------------------------------------------------- */
/*  Shared row primitives                                                      */
/* -------------------------------------------------------------------------- */

const Row = DeploymentRow;
const SectionHeader = DeploymentSectionHeader;

const CodeRow: FC<{ label: string; value: string; children?: ReactNode }> = ({
  label,
  value,
  children,
}) => (
  <div>
    <SectionHeader label={label} />
    <div className="px-4 py-3 space-y-2">
      <div className="flex items-center gap-2">
        <code className="flex-1 dark:bg-gray-950 bg-gray-100 px-3 py-2 font-mono text-xs min-w-0 overflow-hidden break-all">
          {value}
        </code>
        <CopyButton className="h-7 w-7 shrink-0 border border-gray-200 dark:border-gray-800">
          {value}
        </CopyButton>
      </div>
      {children}
    </div>
  </div>
);

const CodeBlock: FC<{ label: string; code: string; helper?: ReactNode }> = ({
  label,
  code,
  helper,
}) => (
  <div>
    <SectionHeader label={label} />
    <div className="px-4 py-3 space-y-2">
      <div className="relative">
        <pre className="dark:bg-gray-950 bg-gray-100 px-3 py-2 font-mono text-xs overflow-auto">
          {code}
        </pre>
        <div className="absolute top-1 right-1">
          <CopyButton className="h-6 w-6 bg-gray-200 dark:bg-gray-800">
            {code}
          </CopyButton>
        </div>
      </div>
      {helper}
    </div>
  </div>
);

/* -------------------------------------------------------------------------- */
/*  SIP Provider Integration Instructions                                      */
/* -------------------------------------------------------------------------- */

const SipIntegrationInstructions: FC<{
  sipHost?: string;
  assistantId: string;
}> = ({ sipHost, assistantId }) => {
  const sipEndpoint = `sip:${sipHost}`;
  const sipUri = `sip:${assistantId || '{ASSISTANT_ID}'}:{{PROJECT_CREDENTIAL_KEY}}@${sipHost}`;

  return (
    <>
      <CodeRow label="SIP Server Endpoint" value={sipEndpoint}>
        <InputHelper>
          Point your SIP trunk / PBX outbound proxy to this address. Rapida
          accepts SIP INVITE and establishes an RTP media session directly.
        </InputHelper>
      </CodeRow>

      <CodeRow label="SIP URI (Authentication)" value={sipUri}>
        <InputHelper>
          Replace{' '}
          <code className="text-red-600">
            {'{{'}PROJECT_CREDENTIAL_KEY{'}}'}
          </code>{' '}
          with your project API key. Format:{' '}
          <code className="text-red-600">sip:assistantID:apiKey@host</code>
        </InputHelper>
      </CodeRow>

      <SectionHeader label="SIP Configuration" />
      <Row label="Transport">
        <span className="text-sm text-gray-900 dark:text-gray-100">
          UDP, TCP, or TLS
        </span>
      </Row>
      <Row label="Port">
        <span className="text-sm font-mono text-gray-900 dark:text-gray-100">
          5060
        </span>
      </Row>
      <Row label="Codec">
        <span className="text-sm text-gray-900 dark:text-gray-100 text-right">
          G.711 μ-law / A-law + DTMF
        </span>
      </Row>
      <Row label="Authentication">
        <span className="text-sm text-gray-900 dark:text-gray-100 text-right">
          URI-based (assistantID:apiKey)
        </span>
      </Row>
      <Row label="Media">
        <span className="text-sm text-gray-900 dark:text-gray-100">
          RTP (direct)
        </span>
      </Row>

      <CodeBlock
        label="PBX Dial Plan — FreeSWITCH"
        code={`<extension name="rapida-ai">
  <condition field="destination_number" expression="^(\\d+)$">
    <action application="bridge"
            data="sofia/external/${sipUri}"/>
  </condition>
</extension>`}
      />

      <CodeBlock
        label="PBX Dial Plan — Asterisk (pjsip.conf + extensions.conf)"
        code={`; pjsip.conf
[rapida-trunk]
type = endpoint
transport = transport-udp
context = from-rapida
aors = rapida-trunk
outbound_auth = rapida-trunk-auth

[rapida-trunk-auth]
type = auth
auth_type = userpass
username = ${assistantId || '{ASSISTANT_ID}'}
password = <YOUR_API_KEY>

[rapida-trunk]
type = aor
contact = sip:${sipHost}:5060

; extensions.conf
[rapida-outbound]
exten => _X.,1,Dial(PJSIP/\${EXTEN}@rapida-trunk)`}
      />
    </>
  );
};

/* -------------------------------------------------------------------------- */
/*  Asterisk Provider Integration Instructions                                 */
/* -------------------------------------------------------------------------- */

const AsteriskIntegrationInstructions: FC<{
  mediaHost: string;
  audioSocketHost?: string;
  assistantId: string;
}> = ({ mediaHost, audioSocketHost, assistantId }) => {
  const rapidaHostname = useMemo(() => {
    try {
      return new URL(mediaHost).hostname;
    } catch {
      return '<your-rapida-host>';
    }
  }, [mediaHost]);

  const audioSocketHostPart = useMemo(() => {
    if (!audioSocketHost) return '<your-rapida-host>';
    return audioSocketHost.split(':')[0] || '<your-rapida-host>';
  }, [audioSocketHost]);

  const audioSocketPort = useMemo(() => {
    if (!audioSocketHost) return '4573';
    const parts = audioSocketHost.split(':');
    return parts.length > 1 ? parts[1] : '4573';
  }, [audioSocketHost]);

  const webhookUrl = `https://${rapidaHostname}/v1/talk/asterisk/call/${assistantId || '{ASSISTANT_ID}'}?from=\${CALLERID(num)}&x-api-key={{PROJECT_CREDENTIAL_KEY}}`;

  return (
    <>
      <CodeRow
        label="WebSocket — Endpoint"
        value={`wss://${rapidaHostname}/v1/talk/asterisk/ctx/{contextId}`}
      />

      <CodeBlock
        label="WebSocket — Dialplan (extensions.conf)"
        code={`[rapida-inbound-ws]
exten => _X.,1,Answer()
 same => n,Set(CTX=\${CURL(${webhookUrl})})
 same => n,GotoIf($["\${CTX}" = ""]?error)
 same => n,WebSocket(wss://${rapidaHostname}/v1/talk/asterisk/ctx/\${CTX})
 same => n,Hangup()
 same => n(error),Playback(an-error-has-occurred)
 same => n,Hangup()`}
        helper={
          <InputHelper>
            Requires <code>chan_websocket.so</code> (Asterisk 20+). WSS port 443
            — ideal for cloud / NAT traversal.
          </InputHelper>
        }
      />

      <CodeRow label="AudioSocket — Endpoint" value={audioSocketHost ?? ''} />

      <CodeBlock
        label="AudioSocket — Dialplan (extensions.conf)"
        code={`[rapida-inbound]
exten => _X.,1,Answer()
 same => n,Set(CHANNEL(audioreadformat)=slin)
 same => n,Set(CHANNEL(audiowriteformat)=slin)
 same => n,Set(CTX=\${CURL(${webhookUrl})})
 same => n,GotoIf($["\${CTX}" = ""]?error)
 same => n,AudioSocket(\${CTX},${audioSocketHostPart}:${audioSocketPort})
 same => n,Hangup()
 same => n(error),Playback(an-error-has-occurred)
 same => n,Hangup()`}
        helper={
          <InputHelper>
            Requires <code>res_audiosocket.so</code> (Asterisk 16+). Raw TCP
            port {audioSocketPort} — SLIN 16-bit 8 kHz. Best for LAN / private
            network.
          </InputHelper>
        }
      />
    </>
  );
};

/* -------------------------------------------------------------------------- */
/*  Voice Input / Output helpers                                               */
/* -------------------------------------------------------------------------- */

const VoiceInput: FC<{ deployment?: DeploymentAudioProvider }> = ({
  deployment,
}) => (
  <>
    <SectionHeader label="Speech to text" />
    {deployment?.getAudiooptionsList() ? (
      deployment?.getAudiooptionsList().length > 0 && (
        <>
          <Row label="Provider">
            <ProviderPill provider={deployment?.getAudioprovider()} />
          </Row>
          {deployment
            ?.getAudiooptionsList()
            .filter(d => d.getValue())
            .filter(d => d.getKey().startsWith('listen.'))
            .map((detail, index) => (
              <Row key={index} label={detail.getKey()}>
                <span className="text-sm font-mono text-gray-900 dark:text-gray-100 truncate max-w-[200px] text-right">
                  {detail.getValue()}
                </span>
                <CopyButton className="h-6 w-6 shrink-0">
                  {detail.getValue()}
                </CopyButton>
              </Row>
            ))}
        </>
      )
    ) : (
      <div className="px-4 py-3">
        <YellowNoticeBlock>Voice input is not enabled</YellowNoticeBlock>
      </div>
    )}
  </>
);

const VoiceOutput: FC<{ deployment?: DeploymentAudioProvider }> = ({
  deployment,
}) => (
  <>
    <SectionHeader label="Text to speech" />
    {deployment?.getAudiooptionsList() ? (
      deployment?.getAudiooptionsList().length > 0 && (
        <>
          <Row label="Provider">
            <ProviderPill provider={deployment?.getAudioprovider()} />
          </Row>
          {deployment
            ?.getAudiooptionsList()
            .filter(d => d.getValue())
            .filter(d => d.getKey().startsWith('speak.'))
            .map((detail, index) => (
              <Row key={index} label={detail.getKey()}>
                <span className="text-sm font-mono text-gray-900 dark:text-gray-100 truncate max-w-[200px] text-right">
                  {detail.getValue()}
                </span>
                <CopyButton className="h-6 w-6 shrink-0">
                  {detail.getValue()}
                </CopyButton>
              </Row>
            ))}
        </>
      )
    ) : (
      <div className="px-4 py-3">
        <YellowNoticeBlock>Voice output is not enabled</YellowNoticeBlock>
      </div>
    )}
  </>
);
