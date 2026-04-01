import {
  AssistantApiDeployment,
  DeploymentAudioProvider,
} from '@rapidaai/react';
import { Tab } from '@/app/components/tab';
import { cn } from '@/utils';
import { ModalProps } from '@/app/components/base/modal';
import { RightSideModal } from '@/app/components/base/modal/right-side-modal';
import { CopyButton } from '@/app/components/carbon/button/copy-button';
import { YellowNoticeBlock } from '@/app/components/container/message/notice-block';
import { ProviderPill } from '@/app/components/pill/provider-model-pill';
import { FC } from 'react';
import { DeploymentRow, DeploymentSectionHeader } from '@/app/components/base/modal/deployment-modal-primitives';

interface AssistantApiDeploymentDialogProps extends ModalProps {
  deployment: AssistantApiDeployment;
}

export function AssistantApiDeploymentDialog(
  props: AssistantApiDeploymentDialogProps,
) {
  return (
    <RightSideModal
      modalOpen={props.modalOpen}
      setModalOpen={props.setModalOpen}
      className="w-2/3 xl:w-1/3 flex-1"
    >
      <div className="h-12 px-4 flex items-center gap-2 border-b border-gray-200 dark:border-gray-800 shrink-0">
        <span className="text-xs font-medium uppercase tracking-[0.08em] text-gray-500 dark:text-gray-400">Deployment</span>
        <span className="text-gray-300 dark:text-gray-600">/</span>
        <span className="text-xs font-medium uppercase tracking-[0.08em] text-gray-500 dark:text-gray-400">SDK / API</span>
        <span className="text-gray-300 dark:text-gray-600">/</span>
        <span className="text-sm font-semibold text-gray-900 dark:text-gray-100 font-mono truncate">{props.deployment.getId()}</span>
      </div>
      <div className="flex flex-col flex-1 overflow-auto h-[calc(100vh-48px)]">
        <Tab
          active="Audio"
          className={cn('bg-white dark:bg-gray-900 sticky top-0 z-1')}
          tabs={[
            {
              label: 'Audio',
              element: (
                <div className="divide-y divide-gray-200 dark:divide-gray-800 w-full">
                  <VoiceInput deployment={props.deployment?.getInputaudio()} />
                  <VoiceOutput deployment={props.deployment?.getOutputaudio()} />
                </div>
              ),
            },
          ]}
        />
      </div>
    </RightSideModal>
  );
}

const Row = DeploymentRow;
const SectionHeader = DeploymentSectionHeader;

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
