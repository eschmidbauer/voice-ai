import { Code, Settings, Microphone, VolumeUp } from '@carbon/icons-react';
import { Assistant } from '@rapidaai/react';
import { toHumanReadableDateTime } from '@/utils/date';
import { DeploymentCard } from './deployment-card';

type ApiEditSection = 'experience' | 'voice-input' | 'voice-output';

interface ApiDeploymentCardProps {
  assistant: Assistant;
  onEdit: () => void;
  onEditSection: (section: ApiEditSection) => void;
  onDetails: () => void;
}

export function ApiDeploymentCard({
  assistant,
  onEdit,
  onEditSection,
  onDetails,
}: ApiDeploymentCardProps) {
  const deployment = assistant.getApideployment()!;
  return (
    <DeploymentCard
      icon={<Code size={24} />}
      title="SDK / API"
      description="Integrate via React SDK or REST API"
      status="DEPLOYED"
      info={[
        { label: 'SDK', value: 'React' },
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
      onEdit={onEdit}
      onDetails={onDetails}
    />
  );
}
