import { Debug, Settings, Microphone, VolumeUp } from '@carbon/icons-react';
import { Assistant } from '@rapidaai/react';
import { toHumanReadableDateTime } from '@/utils/date';
import { DeploymentCard } from './deployment-card';

type DebuggerEditSection = 'experience' | 'voice-input' | 'voice-output';

interface DebuggerDeploymentCardProps {
  assistant: Assistant;
  onEditSection: (section: DebuggerEditSection) => void;
  onPreview: () => void;
  onDetails: () => void;
}

export function DebuggerDeploymentCard({
  assistant,
  onEditSection,
  onPreview,
  onDetails,
}: DebuggerDeploymentCardProps) {
  const deployment = assistant.getDebuggerdeployment()!;
  return (
    <DeploymentCard
      icon={<Debug size={24} />}
      title="Debugger"
      description="Internal testing and debugging"
      status="DEPLOYED"
      info={[
        {
          label: 'Input',
          value: `Text${deployment.getInputaudio() ? ' + Audio' : ''}`,
        },
        {
          label: 'Output',
          value: `Text${deployment.getOutputaudio() ? ' + Audio' : ''}`,
        },
        {
          label: 'Updated',
          value: toHumanReadableDateTime(deployment.getCreateddate()!),
        },
      ]}
      editMenuItems={[
        { label: 'Experience', renderIcon: Settings, onClick: () => onEditSection('experience') },
        { label: 'Voice Input', renderIcon: Microphone, onClick: () => onEditSection('voice-input') },
        { label: 'Voice Output', renderIcon: VolumeUp, onClick: () => onEditSection('voice-output') },
      ]}
      onEdit={() => onEditSection('experience')}
      onPreview={onPreview}
      onDetails={onDetails}
    />
  );
}
